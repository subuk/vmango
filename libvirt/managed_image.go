package libvirt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"subuk/vmango/compute"
	"sync"

	vir "libvirt.org/go/libvirt"
	xml "libvirt.org/go/libvirtxml"
)

func (repo *VolumeRepository) BindImage(node, scope, pool, name string, required uint64) (compute.ImageBinding, error) {
	b := compute.ImageBinding{Node: node, Scope: scope, Pool: pool, Name: name}
	conn, err := repo.pool.Acquire(node)
	if err != nil {
		return b, err
	}
	defer repo.pool.Release(node)
	p, err := conn.LookupStoragePoolByName(pool)
	if err != nil {
		return b, repositoryError(err, "lookup image_pool")
	}
	defer p.Free()
	active, err := p.IsActive()
	if err != nil {
		return b, err
	}
	if !active {
		return b, fmt.Errorf("%w: image_pool %q is inactive", compute.ErrConflict, pool)
	}
	description, err := p.GetXMLDesc(0)
	if err != nil {
		return b, err
	}
	var config xml.StoragePool
	if err = config.Unmarshal(description); err != nil {
		return b, err
	}
	if config.Type != "dir" || config.Target == nil || config.Target.Path == "" {
		return b, fmt.Errorf("%w: image_pool %q must be a dir pool", compute.ErrInvalid, pool)
	}
	info, err := p.GetInfo()
	if err != nil {
		return b, err
	}
	if required > info.Available {
		return b, fmt.Errorf("%w: image_pool %q needs %d available bytes, has %d", compute.ErrConflict, pool, required, info.Available)
	}
	b.PoolID, err = p.GetUUIDString()
	if err != nil {
		return b, err
	}
	b.Path = path.Join(config.Target.Path, name)
	return b, nil
}
func (repo *VolumeRepository) InspectImage(b compute.ImageBinding) (*compute.Volume, string, error) {
	actual, err := repo.BindImage(b.Node, b.Scope, b.Pool, b.Name, 0)
	if err != nil {
		return nil, "", err
	}
	if actual != b {
		return nil, "", errors.New("image storage binding changed")
	}
	conn, err := repo.pool.Acquire(b.Node)
	if err != nil {
		return nil, "", err
	}
	defer repo.pool.Release(b.Node)
	p, err := conn.LookupStoragePoolByName(b.Pool)
	if err != nil {
		return nil, "", repositoryError(err, "lookup image pool")
	}
	defer p.Free()
	if err = p.Refresh(0); err != nil {
		return nil, "", repositoryError(err, "refresh image pool")
	}
	v, err := p.LookupStorageVolByName(b.Name)
	if err != nil {
		return nil, "", repositoryError(err, "inspect image volume")
	}
	defer v.Free()
	description, err := v.GetXMLDesc(0)
	if err != nil {
		return nil, "", err
	}
	var config xml.StorageVolume
	if err = config.Unmarshal(description); err != nil {
		return nil, "", err
	}
	if config.Target == nil || config.Target.Timestamps == nil || config.Target.Timestamps.Mtime == "" || config.Target.Timestamps.Ctime == "" {
		return nil, "", errors.New("image backend must expose modification/change timestamps for verified reuse")
	}
	if config.BackingStore != nil && config.BackingStore.Path != "" {
		return nil, "", errors.New("managed images cannot reference backing files")
	}
	volume, err := repo.virVolumeToVolume(b.Node, p, v)
	if err != nil {
		return nil, "", err
	}
	if err = repo.fetchAttachedVm(conn, []*compute.Volume{volume}); err != nil {
		return nil, "", err
	}
	if volume.AttachedTo != "" {
		return nil, "", errors.New("managed source is attached to a machine; refusing reuse")
	}
	key, err := v.GetKey()
	if err != nil {
		return nil, "", err
	}
	identity := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%d\x00%d", b.PoolID, key, config.Target.Timestamps.Mtime, config.Target.Timestamps.Ctime, volume.Format, volume.Size.Bytes())
	h := sha256.Sum256([]byte(identity))
	return volume, hex.EncodeToString(h[:]), nil
}
func (repo *VolumeRepository) CreateImage(b compute.ImageBinding, size uint64) error {
	actual, err := repo.BindImage(b.Node, b.Scope, b.Pool, b.Name, size)
	if err != nil {
		return err
	}
	if actual != b {
		return errors.New("image pool changed")
	}
	conn, err := repo.pool.Acquire(b.Node)
	if err != nil {
		return err
	}
	defer repo.pool.Release(b.Node)
	p, err := conn.LookupStoragePoolByName(b.Pool)
	if err != nil {
		return err
	}
	defer p.Free()
	if existing, lookupErr := p.LookupStorageVolByName(b.Name); lookupErr == nil {
		existing.Free()
		return fmt.Errorf("%w: managed destination already exists", compute.ErrConflict)
	} else if !errors.Is(lookupErr, vir.ERR_NO_STORAGE_VOL) {
		return repositoryError(lookupErr, "check image destination")
	}
	info, err := p.GetInfo()
	if err != nil {
		return err
	}
	if size > info.Available {
		return fmt.Errorf("%w: insufficient image pool capacity", compute.ErrConflict)
	}
	// Reserve the artifact's physical bytes on the node. Upload replaces this raw
	// staging content; libvirt then probes the resulting raw/QCOW2 format.
	config := xml.StorageVolume{Name: b.Name, Capacity: &xml.StorageVolumeSize{Unit: "bytes", Value: size}, Allocation: &xml.StorageVolumeSize{Unit: "bytes", Value: size}, Target: &xml.StorageVolumeTarget{Format: &xml.StorageVolumeTargetFormat{Type: "raw"}}}
	description, err := config.Marshal()
	if err != nil {
		return err
	}
	volume, err := p.StorageVolCreateXML(description, 0)
	if err != nil {
		return repositoryError(err, "create image staging")
	}
	defer volume.Free()
	return nil
}
func (repo *VolumeRepository) UploadImage(ctx context.Context, b compute.ImageBinding, content io.Reader, size uint64) error {
	actual, err := repo.BindImage(b.Node, b.Scope, b.Pool, b.Name, 0)
	if err != nil {
		return err
	}
	if actual != b {
		return errors.New("image pool changed before upload")
	}
	// A transfer gets its own connection. The ordinary connection mutex must not
	// block task submissions or unrelated operations for the duration of HTTP IO.
	uri, ok := repo.pool.nodeUri[b.Node]
	if !ok {
		return compute.ErrUnknownNode
	}
	conn, err := vir.NewConnect(uri)
	if err != nil {
		return repositoryError(err, "open image upload connection")
	}
	defer conn.Close()
	volume, err := conn.LookupStorageVolByPath(b.Path)
	if err != nil {
		return repositoryError(err, "lookup image staging")
	}
	defer volume.Free()
	return uploadStream(ctx, conn, volume, content, size)
}
func uploadStream(ctx context.Context, conn *vir.Connect, volume *vir.StorageVol, content io.Reader, size uint64) error {
	stream, err := conn.NewStream(0)
	if err != nil {
		return repositoryError(err, "create upload stream")
	}
	defer stream.Free()
	if err = volume.Upload(stream, 0, size, 0); err != nil {
		return repositoryError(err, "start upload")
	}
	// Abort also unblocks a stalled Send/Finish when cancellation or timeout fires.
	var once sync.Once
	abort := func() { once.Do(func() { stream.Abort() }) }
	stop := context.AfterFunc(ctx, abort)
	defer func() {
		if !stop() {
			abort()
		}
		once.Do(func() {})
	}()
	finished := false
	defer func() {
		if !finished {
			abort()
		}
	}()
	if _, err = io.CopyBuffer(&virStreamWrapper{stream}, content, make([]byte, 128*1024)); err != nil {
		return repositoryError(err, "stream image bytes")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = stream.Finish(); err != nil {
		return repositoryError(err, "finish image stream")
	}
	finished = true
	return nil
}
func (repo *VolumeRepository) DeleteImage(b compute.ImageBinding) error {
	if !strings.HasPrefix(b.Name, compute.ManagedImagePrefix) {
		return errors.New("refusing to delete unowned image destination")
	}
	actual, err := repo.BindImage(b.Node, b.Scope, b.Pool, b.Name, 0)
	if err != nil {
		return err
	}
	if actual != b {
		return errors.New("image pool changed; refusing cleanup")
	}
	conn, err := repo.pool.Acquire(b.Node)
	if err != nil {
		return err
	}
	defer repo.pool.Release(b.Node)
	p, err := conn.LookupStoragePoolByName(b.Pool)
	if err != nil {
		return err
	}
	defer p.Free()
	v, err := p.LookupStorageVolByName(b.Name)
	if err != nil {
		return repositoryError(err, "lookup owned image staging")
	}
	defer v.Free()
	volume, err := repo.virVolumeToVolume(b.Node, p, v)
	if err != nil {
		return err
	}
	if err = repo.fetchAttachedVm(conn, []*compute.Volume{volume}); err != nil {
		return err
	}
	if volume.AttachedTo != "" {
		return errors.New("owned staging is attached to a machine; refusing cleanup")
	}
	return repositoryError(v.Delete(0), "delete owned incomplete image")
}
