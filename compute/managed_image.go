package compute

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net"
	"net/http"
	"strings"
	"subuk/vmango/tasks"
	"sync"
	"time"
)

const ManagedImagePrefix = "__vmango_image_"

var ErrArtifactNotFound = errors.New("artifact metadata not found")

// ImageBinding is pinned at acceptance, including the actual backend pool UUID.
// Scope identifies the connection rather than its configurable display name.
type ImageBinding struct{ Node, Scope, Pool, PoolID, Name, Path string }
type ImageArtifact struct {
	Binding     ImageBinding
	SHA256      string
	Format      VolumeFormat
	Owner       string
	State       string
	Fingerprint string
	Capacity    uint64
	SizeBytes   uint64
	Error       string
	FailedAt    time.Time
}
type ImageArtifactRepository interface {
	Get(key string) (ImageArtifact, error)
	Put(key string, artifact ImageArtifact) error
}

// ManagedImageBackend owns pool validation, stream lifecycle and backend identity.
// Image payloads must never be written on the application host.
type ManagedImageBackend interface {
	BindImage(node, scope, pool, name string, required uint64) (ImageBinding, error)
	InspectImage(ImageBinding) (*Volume, string, error)
	CreateImage(ImageBinding, uint64) error
	UploadImage(context.Context, ImageBinding, io.Reader, uint64) error
	DeleteImage(ImageBinding) error
}
type ImageTransferLimits struct {
	MaxBytes             uint64
	Timeout, IdleTimeout time.Duration
}
type imageFlight struct {
	done chan struct{}
	err  error
}
type ManagedImageService struct {
	backend  ManagedImageBackend
	store    ImageArtifactRepository
	bindings map[string]ImageBinding
	limits   ImageTransferLimits
	client   *http.Client
	mu       sync.Mutex
	flights  map[string]*imageFlight
}

func NewManagedImageService(backend ManagedImageBackend, store ImageArtifactRepository, bindings map[string]ImageBinding, limits ImageTransferLimits) *ManagedImageService {
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext, TLSHandshakeTimeout: 30 * time.Second, ResponseHeaderTimeout: 30 * time.Second, DisableCompression: true}
	return &ManagedImageService{backend: backend, store: store, bindings: bindings, limits: limits, flights: map[string]*imageFlight{}, client: &http.Client{Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || (req.URL.Scheme != "http" && req.URL.Scheme != "https") || req.URL.User != nil || (via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https") {
			return errors.New("unsupported image redirect")
		}
		return nil
	}}}
}
func sameImageBinding(a, b ImageBinding) bool { a.Node = ""; b.Node = ""; return a == b }

func artifactKey(b ImageBinding) string {
	h := sha256.Sum256([]byte(b.Scope + "\x00" + b.PoolID + "\x00" + b.Path))
	return hex.EncodeToString(h[:])
}
func (s *ManagedImageService) Bind(node string, i Image) (ImageBinding, error) {
	b, ok := s.bindings[node]
	if !ok {
		return b, fmt.Errorf("image storage is not configured on node %q", node)
	}
	name := ManagedImagePrefix + strings.ToLower(i.Download.SHA256) + "." + i.Download.Format.String()
	return s.backend.BindImage(node, b.Scope, b.Pool, name, 0)
}
func (s *ManagedImageService) ready(b ImageBinding, i Image) (*Volume, error) {
	a, err := s.store.Get(artifactKey(b))
	if err != nil {
		return nil, err
	}
	if a.State == "absent" {
		return nil, ErrArtifactNotFound
	}
	if a.State != "ready" || a.SHA256 != i.Download.SHA256 || a.Format != i.Download.Format || !sameImageBinding(a.Binding, b) {
		return nil, fmt.Errorf("image preparation is incomplete; inspect its task and owned destination before a new attempt")
	}
	if i.Download.SizeBytes != 0 && i.Download.SizeBytes != a.SizeBytes {
		return nil, errors.New("source_size_bytes differs from the verified shared artifact")
	}
	v, fingerprint, err := s.backend.InspectImage(b)
	if err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrVolumeNotFound) {
			return nil, ErrArtifactNotFound
		}
		return nil, err
	}
	if fingerprint == "" || fingerprint != a.Fingerprint || v.Size.Bytes() != a.Capacity || v.Format != a.Format {
		return nil, fmt.Errorf("managed image no longer matches its verified backend identity; refusing reuse")
	}
	return v, nil
}

// Status performs no upstream HTTP requests and never creates storage.
func (s *ManagedImageService) Status(b ImageBinding, i Image) (string, string) {
	s.mu.Lock()
	f := s.flights[artifactKey(b)]
	s.mu.Unlock()
	if f != nil {
		return "preparing", ""
	}
	if a, err := s.store.Get(artifactKey(b)); err == nil && a.State == "absent" && a.Error != "" {
		return "failed", a.Error + " A new creation can retry the transfer."
	}
	if _, err := s.ready(b, i); err == nil {
		return "ready", ""
	} else if !errors.Is(err, ErrArtifactNotFound) {
		return "failed", err.Error()
	}
	if _, _, err := s.backend.InspectImage(b); errors.Is(err, ErrNotFound) || errors.Is(err, ErrVolumeNotFound) {
		return "downloadable", ""
	} else if err != nil {
		return "failed", err.Error()
	}
	return "failed", "An existing volume has no verified image record; it will not be adopted or overwritten."
}

// Ensure runs inside a bounded machine.create worker. The first worker owns
// preparation; followers wait on that active worker, never on queued work.
func (s *ManagedImageService) Ensure(ctx context.Context, b ImageBinding, i Image, e *tasks.Execution) error {
	if current, ok := s.bindings[b.Node]; !ok || current.Scope != b.Scope {
		return errors.New("accepted image node connection changed; inspect the task and submit a new command")
	}
	key := artifactKey(b)
	s.mu.Lock()
	if f := s.flights[key]; f != nil {
		s.mu.Unlock()
		ref := VolumeRef(b.Node, b.Path)
		return e.Stage("wait-image", &ref, false, func() error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-f.done:
				if f.err != nil {
					return f.err
				}
				_, err := s.ready(b, i)
				return err
			}
		})
	}
	f := &imageFlight{done: make(chan struct{})}
	s.flights[key] = f
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.flights, key); close(f.done); s.mu.Unlock() }()
	f.err = s.prepare(ctx, b, i, e)
	return f.err
}
func (s *ManagedImageService) prepare(ctx context.Context, b ImageBinding, i Image, e *tasks.Execution) (err error) {
	ref := VolumeRef(b.Node, b.Path)
	var exists bool
	if err = e.Stage("prepare-image", &ref, false, func() error {
		actual, bindErr := s.backend.BindImage(b.Node, b.Scope, b.Pool, b.Name, 0)
		if bindErr != nil {
			return bindErr
		}
		if actual != b {
			return errors.New("image pool binding changed since task acceptance")
		}
		if previous, getErr := s.store.Get(artifactKey(b)); getErr == nil && previous.State == "absent" && !previous.FailedAt.IsZero() && e.AcceptedAt().Before(previous.FailedAt) {
			return fmt.Errorf("shared image preparation failed: %s Submit a new creation explicitly to retry", previous.Error)
		}
		if _, readyErr := s.ready(b, i); readyErr == nil {
			exists = true
			return nil
		} else if !errors.Is(readyErr, ErrArtifactNotFound) {
			return readyErr
		}
		if _, _, inspectErr := s.backend.InspectImage(b); inspectErr == nil {
			return fmt.Errorf("%w: image destination exists without verification; refusing overwrite", ErrConflict)
		} else if !errors.Is(inspectErr, ErrNotFound) && !errors.Is(inspectErr, ErrVolumeNotFound) {
			return inspectErr
		}
		return nil
	}); err != nil || exists {
		return err
	}
	parentContext := ctx
	ctx, cancel := context.WithTimeout(ctx, s.limits.Timeout)
	defer cancel()
	transferError := func(cause error) error {
		if cause != nil && ctx.Err() != nil && parentContext.Err() == nil {
			return fmt.Errorf("image transfer timeout exceeded (total %s, idle %s); inspect destination staging before retrying", s.limits.Timeout, s.limits.IdleTimeout)
		}
		return cause
	}
	a := ImageArtifact{Binding: b, SHA256: i.Download.SHA256, Format: i.Download.Format, Owner: e.ID(), State: "incomplete"}
	created := false
	defer func() {
		if err == nil {
			return
		}
		err = transferError(err)
		a.Error = tasks.Redact(err.Error(), []string{i.Download.URL})
		a.FailedAt = time.Now().UTC()
		a.State = "failed"
		if !created {
			a.State = "absent"
		}
		// Shutdown or uncertain stream completion leaves owned staging for explicit inspection.
		if created && ctx.Err() == nil {
			cleanupErr := e.Stage("cleanup-image", &ref, true, func() error {
				if cleanupErr := s.backend.DeleteImage(b); cleanupErr != nil {
					return cleanupErr
				}
				return e.Effect(ref, "deleted-incomplete-image")
			})
			if cleanupErr != nil {
				err = errors.Join(err, fmt.Errorf("image staging cleanup failed: %w", cleanupErr))
			} else {
				a.State = "absent"
			}
		}
		if saveErr := s.store.Put(artifactKey(b), a); saveErr != nil {
			err = errors.Join(err, saveErr)
		}
	}()
	// An ordinary failed attempt whose owned staging was confirmed deleted may be retried.
	var body io.ReadCloser
	var header []byte
	var size, capacity uint64
	if err = e.Stage("inspect-image-source", &ref, false, func() error {
		req, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, i.Download.URL, nil)
		if requestErr != nil {
			return errors.New("invalid source URL")
		}
		req.Header.Set("Accept-Encoding", "identity")
		response, requestErr := s.client.Do(req)
		if requestErr != nil {
			return transferError(errors.New("image HTTP request failed; check source connectivity and timeout"))
		}
		body = response.Body
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("image source returned HTTP %d", response.StatusCode)
		}
		if ce := response.Header.Get("Content-Encoding"); ce != "" && ce != "identity" {
			return fmt.Errorf("image source HTTP encoding %q is unsupported; use direct raw/qcow2 bytes", ce)
		}
		if response.ContentLength > 0 {
			size = uint64(response.ContentLength)
		}
		if i.Download.SizeBytes > 0 {
			if size > 0 && size != i.Download.SizeBytes {
				return fmt.Errorf("source_size_bytes disagrees with HTTP Content-Length")
			}
			size = i.Download.SizeBytes
		}
		if size == 0 {
			return fmt.Errorf("cannot determine artifact byte size; set source_size_bytes on image %q", i.ID)
		}
		if size > s.limits.MaxBytes {
			return fmt.Errorf("image exceeds image_transfers.max_bytes (%d)", s.limits.MaxBytes)
		}
		timer := time.AfterFunc(s.limits.IdleTimeout, cancel)
		defer timer.Stop()
		header = make([]byte, min(size, 104))
		if _, readErr := io.ReadFull(body, header); readErr != nil {
			return transferError(fmt.Errorf("truncated image header: %w", readErr))
		}
		var inspectErr error
		capacity, inspectErr = inspectImagePrefix(header, i.Download.Format, size)
		if inspectErr != nil {
			return inspectErr
		}
		actual, bindErr := s.backend.BindImage(b.Node, b.Scope, b.Pool, b.Name, size)
		if bindErr != nil {
			return bindErr
		}
		if actual != b {
			return errors.New("image pool binding changed since task acceptance")
		}
		return nil
	}); body != nil {
		defer body.Close()
	}
	if err != nil {
		return err
	}
	a.SizeBytes = size
	if err = e.Stage("create-image-staging", &ref, true, func() error {
		if saveErr := s.store.Put(artifactKey(b), a); saveErr != nil {
			return saveErr
		}
		if createErr := s.backend.CreateImage(b, size); createErr != nil {
			return createErr
		}
		created = true
		return e.Effect(ref, "created-image-staging")
	}); err != nil {
		return err
	}
	if err = e.Stage("stream-image", &ref, true, func() error {
		reader := &verifiedImageReader{reader: io.MultiReader(bytes.NewReader(header), body), hash: sha256.New(), expected: i.Download.SHA256, size: size, progress: e, start: time.Now(), idle: s.limits.IdleTimeout}
		reader.timer = time.AfterFunc(reader.idle, cancel)
		defer reader.timer.Stop()
		if uploadErr := s.backend.UploadImage(ctx, b, reader, size); uploadErr != nil {
			return transferError(uploadErr)
		}
		if !reader.complete {
			return errors.New("image upload did not consume and verify the complete source")
		}
		return e.Effect(ref, "uploaded-image")
	}); err != nil {
		return err
	}
	return e.Stage("verify-image", &ref, true, func() error {
		v, fingerprint, verifyErr := s.backend.InspectImage(b)
		if verifyErr != nil {
			return transferError(verifyErr)
		}
		if ctx.Err() != nil {
			return transferError(ctx.Err())
		}
		if v.Format != i.Download.Format || v.Size.Bytes() != capacity || fingerprint == "" {
			return errors.New("backend did not confirm image format, capacity and identity")
		}
		a.State = "ready"
		a.Fingerprint = fingerprint
		a.Capacity = v.Size.Bytes()
		if saveErr := s.store.Put(artifactKey(b), a); saveErr != nil {
			return saveErr
		}
		return e.Effect(ref, "verified-image-ready")
	})
}

// verifiedImageReader verifies EOF and checksum before allowing libvirt Finish.
// At most one bounded prefix plus the upload copy buffer is held in memory.
type verifiedImageReader struct {
	reader      io.Reader
	hash        hash.Hash
	expected    string
	size, count uint64
	complete    bool
	progress    *tasks.Execution
	start, last time.Time
	idle        time.Duration
	timer       *time.Timer
}

func (r *verifiedImageReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	r.timer.Reset(r.idle)
	if uint64(len(p)) > r.size-r.count {
		p = p[:r.size-r.count]
	}
	if len(p) == 0 {
		var extra [1]byte
		n, err := r.reader.Read(extra[:])
		if n != 0 {
			return 0, errors.New("image response exceeds expected source byte size")
		}
		if err != io.EOF {
			if err == nil {
				err = io.ErrNoProgress
			}
			return 0, err
		}
		if hex.EncodeToString(r.hash.Sum(nil)) != r.expected {
			return 0, errors.New("image SHA-256 mismatch; destination remains unverified")
		}
		r.complete = true
		if err = r.report(true); err != nil {
			return 0, err
		}
		return 0, io.EOF
	}
	n, err := r.reader.Read(p)
	if n > 0 {
		r.hash.Write(p[:n])
		r.count += uint64(n)
		if reportErr := r.report(false); reportErr != nil {
			return n, reportErr
		}
	}
	if err == io.EOF {
		if r.count != r.size {
			return n, io.ErrUnexpectedEOF
		}
		err = nil
	}
	return n, err
}
func (r *verifiedImageReader) report(final bool) error {
	now := time.Now()
	if !final && !r.last.IsZero() && now.Sub(r.last) < time.Second {
		return nil
	}
	r.last = now
	speed := float64(r.count) / max(now.Sub(r.start).Seconds(), 0.001)
	p := tasks.TransferProgress{Bytes: r.count, TotalBytes: r.size, BytesPerSecond: speed, UpdatedAt: now.UTC()}
	if speed > 0 {
		remaining := uint64(float64(r.size-r.count) / speed)
		p.RemainingSeconds = &remaining
	}
	return r.progress.Transfer(p)
}
func inspectImagePrefix(p []byte, format VolumeFormat, size uint64) (uint64, error) {
	qcow := len(p) >= 4 && bytes.Equal(p[:4], []byte{'Q', 'F', 'I', 0xfb})
	if format == VolumeFormatRaw {
		if qcow || bytes.HasPrefix(p, []byte{0x1f, 0x8b}) || bytes.HasPrefix(p, []byte{0xfd, '7', 'z', 'X', 'Z', 0}) || bytes.HasPrefix(p, []byte{'P', 'K', 3, 4}) {
			return 0, errors.New("source_format raw disagrees with image or archive header")
		}
		return size, nil
	}
	if !qcow || len(p) < 72 {
		return 0, errors.New("expected a directly usable QCOW2 image; archives and conversion are unsupported")
	}
	version := binary.BigEndian.Uint32(p[4:8])
	capacity := binary.BigEndian.Uint64(p[24:32])
	if (version != 2 && version != 3) || capacity == 0 || binary.BigEndian.Uint64(p[8:16]) != 0 || binary.BigEndian.Uint32(p[16:20]) != 0 || binary.BigEndian.Uint32(p[32:36]) != 0 {
		return 0, errors.New("unsupported QCOW2 version, backing file, encryption or capacity")
	}
	if version == 3 && (len(p) < 104 || binary.BigEndian.Uint64(p[72:80]) != 0) {
		return 0, errors.New("QCOW2 incompatible features are unsupported")
	}
	return capacity, nil
}
