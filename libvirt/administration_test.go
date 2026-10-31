package libvirt

import (
	"context"
	"errors"
	"strings"
	"subuk/vmango/catalog"
	"subuk/vmango/compute"
	"testing"
)

type administrationNodes struct {
	compute.NodeRepository
	offline map[string]bool
}

func (n *administrationNodes) Get(node string, _ compute.NodeGetOptions) (*compute.Node, error) {
	if n.offline[node] {
		return nil, compute.ErrUnavailable
	}
	return &compute.Node{Id: node, CpuArch: compute.ArchAmd64}, nil
}

type administrationMachines struct {
	compute.VirtualMachineRepository
}

func (m *administrationMachines) Capabilities(node string, arch compute.Arch) (compute.VirtualMachineCapabilities, error) {
	if node == "unsupported" {
		arch = compute.ArchAarch64
	}
	return compute.VirtualMachineCapabilities{Arch: arch, Graphics: []compute.GraphicType{compute.GraphicTypeVnc}, VideoModels: []compute.VideoModel{compute.VideoModelNone}}, nil
}

type administrationVolumes struct {
	compute.VolumeRepository
	volumes map[string]*compute.Volume
	fail    map[string]error
	reads   int
}

func (v *administrationVolumes) Get(path, node string) (*compute.Volume, error) {
	v.reads++
	if err := v.fail[node]; err != nil {
		return nil, err
	}
	if vol := v.volumes[node]; vol != nil {
		return vol, nil
	}
	return nil, compute.ErrNotFound
}
func TestAdministrationImageAvailabilityKeepsIndependentStates(t *testing.T) {
	repo, err := catalog.NewImageRepository([]compute.Image{{ID: "image", CloudID: "cloud", Name: "Image", Arch: compute.ArchAmd64, SourcePath: "/source"}})
	if err != nil {
		t.Fatal(err)
	}
	volumes := &administrationVolumes{volumes: map[string]*compute.Volume{"ready": {Path: "/source"}, "unsupported": {Path: "/source"}, "hidden": {Metadata: compute.VolumeMetadata{Hidden: true}}, "attached": {AttachedTo: "machine"}}, fail: map[string]error{"storage-offline": compute.ErrUnavailable}}
	nodes := &administrationNodes{offline: map[string]bool{"offline": true}}
	adapter := &AdministrationObservations{Nodes: compute.NewNodeService(nodes), VMs: compute.NewVirtualMachineService(&administrationMachines{}), Volumes: compute.NewVolumeService(volumes), Images: compute.NewImageService(repo)}
	hosts := []compute.AdministrationHostReference{}
	for _, id := range []string{"ready", "unsupported", "hidden", "attached", "missing", "storage-offline", "offline"} {
		hosts = append(hosts, compute.AdministrationHostReference{ID: id})
	}
	out, err := adapter.ImageAvailability(context.Background(), "cloud", "image", hosts)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]compute.AdministrationImageHost{}
	for _, h := range out.Libvirt.Hosts {
		states[h.HostID] = h
	}
	if len(states) != len(hosts) {
		t.Fatal(out)
	}
	for host, want := range map[string]string{"ready": "ready", "unsupported": "ready", "hidden": "hidden", "attached": "attached", "missing": "missing", "storage-offline": "unknown", "offline": "unknown"} {
		if states[host].SourceState != want {
			t.Fatal(host, states[host])
		}
	}
	if states["unsupported"].Compatibility != "unsupported" || states["ready"].Compatibility != "supported" || states["offline"].Compatibility != "unknown" || states["offline"].Observation != "unavailable" || states["storage-offline"].Observation != "unavailable" {
		t.Fatal(states)
	}
	nodes.offline = map[string]bool{}
	for _, h := range hosts {
		nodes.offline[h.ID] = true
	}
	out, err = adapter.ImageAvailability(context.Background(), "cloud", "image", hosts)
	if err != nil || len(out.Libvirt.Hosts) != len(hosts) {
		t.Fatal(out, err)
	}
	for _, h := range out.Libvirt.Hosts {
		if h.Observation != "unavailable" || h.SourceState != "unknown" {
			t.Fatal(h)
		}
	}
}

type administrationArtifacts struct {
	compute.ImageArtifactRepository
	artifact compute.ImageArtifact
}

func (a *administrationArtifacts) Get(string) (compute.ImageArtifact, error) { return a.artifact, nil }

type administrationManagedBackend struct {
	compute.ManagedImageBackend
	binding compute.ImageBinding
	volume  *compute.Volume
}

func (b *administrationManagedBackend) BindImage(node, scope, pool, name string, _ uint64) (compute.ImageBinding, error) {
	return b.binding, nil
}
func (b *administrationManagedBackend) InspectImage(compute.ImageBinding) (*compute.Volume, string, error) {
	return b.volume, "fingerprint", nil
}
func TestAdministrationManagedAvailabilityDoesNotTrustReadyForAttachment(t *testing.T) {
	hash := strings.Repeat("a", 64)
	image := compute.Image{ID: "image", CloudID: "cloud", Name: "Image", Arch: compute.ArchAmd64, Download: &compute.ImageDownload{URL: "https://images.test/private?token=secret", SHA256: hash, Format: compute.VolumeFormatQcow2, SizeBytes: 1024}}
	repo, err := catalog.NewImageRepository([]compute.Image{image})
	if err != nil {
		t.Fatal(err)
	}
	binding := compute.ImageBinding{Node: "cache", Scope: "scope", Pool: "pool", PoolID: "pool-id", Name: compute.ManagedImagePrefix + hash + ".qcow2", Path: "/cache"}
	cache := &compute.Volume{NodeId: "cache", Path: "/cache", Pool: "pool", Format: compute.VolumeFormatQcow2, Size: compute.NewSize(4096, compute.SizeUnitB), Metadata: compute.VolumeMetadata{Hidden: true, Protected: true}}
	artifacts := &administrationArtifacts{artifact: compute.ImageArtifact{Binding: binding, State: "ready", SHA256: hash, Format: compute.VolumeFormatQcow2, SizeBytes: 1024, Capacity: 4096, Fingerprint: "fingerprint"}}
	images := compute.NewImageService(repo)
	images.ManagedSources = compute.NewManagedImageService(&administrationManagedBackend{binding: binding, volume: cache}, artifacts, map[string]compute.ImageBinding{"cache": {Scope: "scope", Pool: "pool"}}, compute.ImageTransferLimits{})
	volumes := &administrationVolumes{volumes: map[string]*compute.Volume{"cache": cache}, fail: map[string]error{}}
	adapter := &AdministrationObservations{Nodes: compute.NewNodeService(&administrationNodes{}), VMs: compute.NewVirtualMachineService(&administrationMachines{}), Volumes: compute.NewVolumeService(volumes), Images: images}
	hosts := []compute.AdministrationHostReference{{ID: "cache"}}
	out, err := adapter.ImageAvailability(context.Background(), "cloud", "image", hosts)
	if err != nil || out.Libvirt.Hosts[0].SourceState != "ready" {
		t.Fatal(out, err)
	}
	cache.AttachedTo = "machine"
	out, err = adapter.ImageAvailability(context.Background(), "cloud", "image", hosts)
	if err != nil || out.Libvirt.Hosts[0].SourceState != "attached" {
		t.Fatal(out, err)
	}
	volumes.fail["cache"] = compute.ErrUnavailable
	out, err = adapter.ImageAvailability(context.Background(), "cloud", "image", hosts)
	if err != nil || out.Libvirt.Hosts[0].SourceState != "unknown" || out.Libvirt.Hosts[0].Observation != "unavailable" {
		t.Fatal(out, err)
	}
	volumes.fail["cache"] = compute.ErrNotFound
	out, err = adapter.ImageAvailability(context.Background(), "cloud", "image", hosts)
	if err != nil || out.Libvirt.Hosts[0].SourceState != "missing" {
		t.Fatal(out, err)
	}
}
func TestAdministrationDiagnosticsRedactRemotePathsAndCredentials(t *testing.T) {
	err := administrationFailure(errors.New(`host-a /var/lib/vmango/disk: failed https://user:password@images.test/private-signed-token?query-secret=yes password=secret-password Bearer secret-token`))
	if !errors.Is(err, compute.ErrUnavailable) {
		t.Fatal(err)
	}
	for _, secret := range []string{"user:", "password@", "private-signed-token", "query-secret", "secret-password", "secret-token"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatal("unsafe diagnostic", err)
		}
	}
	if !strings.Contains(err.Error(), "host-a /var/lib/vmango/disk") || !strings.Contains(err.Error(), "images.test") {
		t.Fatal("useful diagnostics were removed", err)
	}
}
