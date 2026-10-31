package libvirt

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"sort"
	"subuk/vmango/compute"
	"subuk/vmango/tasks"
)

// AdministrationObservations uses the existing backend services for reads only.
// Configuration projection never calls this adapter.
type AdministrationObservations struct {
	Nodes           *compute.NodeService
	NetworksService *compute.NetworkService
	PoolsService    *compute.VolumePoolService
	VMs             *compute.VirtualMachineService
	Volumes         *compute.VolumeService
	Images          *compute.ImageService
	Clouds          *compute.CloudService
}

var administrationURL = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^\s<>"']+`)

func administrationFailure(err error) error {
	if err == nil {
		return nil
	}
	message := administrationURL.ReplaceAllStringFunc(err.Error(), func(raw string) string {
		u, e := url.Parse(raw)
		if e != nil {
			return "[redacted URL]"
		}
		return u.Scheme + "://" + u.Hostname() + "/[redacted]"
	})
	return &compute.AdministrationObservationError{Diagnostic: tasks.Redact(message, nil)}
}
func (b *AdministrationObservations) Host(ctx context.Context, cloud, node string) (*compute.Node, error) {
	if b.Nodes == nil {
		return nil, compute.ErrUnavailable
	}
	n, err := b.Nodes.Get(node, compute.NodeGetOptions{})
	return n, administrationFailure(err)
}
func (b *AdministrationObservations) Networks(ctx context.Context, cloud, node string) ([]*compute.Network, error) {
	if _, err := b.Host(ctx, cloud, node); err != nil {
		return nil, err
	}
	if b.NetworksService == nil {
		return nil, compute.ErrUnavailable
	}
	v, err := b.NetworksService.List(compute.NetworkListOptions{NodeIds: []string{node}})
	sort.Slice(v, func(i, j int) bool { return v[i].Name < v[j].Name })
	return v, administrationFailure(err)
}
func (b *AdministrationObservations) Pools(ctx context.Context, cloud, node string) ([]*compute.VolumePool, error) {
	if _, err := b.Host(ctx, cloud, node); err != nil {
		return nil, err
	}
	if b.PoolsService == nil {
		return nil, compute.ErrUnavailable
	}
	v, err := b.PoolsService.List(compute.VolumePoolListOptions{NodeIds: []string{node}})
	sort.Slice(v, func(i, j int) bool { return v[i].Name < v[j].Name })
	return v, administrationFailure(err)
}
func (b *AdministrationObservations) Capabilities(ctx context.Context, cloud, node string, arch compute.Arch) (compute.VirtualMachineCapabilities, error) {
	if b.VMs == nil {
		return compute.VirtualMachineCapabilities{}, compute.ErrUnavailable
	}
	if arch == compute.ArchUnknown {
		n, err := b.Host(ctx, cloud, node)
		if err != nil {
			return compute.VirtualMachineCapabilities{}, err
		}
		arch = n.CpuArch
	}
	c, err := b.VMs.Capabilities(node, arch)
	return c, administrationFailure(err)
}
func (b *AdministrationObservations) Catalog(ctx context.Context, cloud, node string) (compute.HostCatalog, error) {
	if _, err := b.Host(ctx, cloud, node); err != nil {
		return compute.HostCatalog{}, err
	}
	if b.Clouds == nil {
		return compute.HostCatalog{}, compute.ErrUnavailable
	}
	c, err := b.Clouds.HostCatalog(cloud, node, nil)
	return c, administrationFailure(err)
}
func (b *AdministrationObservations) ImageAvailability(ctx context.Context, cloud, image string, hosts []compute.AdministrationHostReference) (compute.AdministrationImageAvailability, error) {
	out := compute.AdministrationImageAvailability{CloudID: cloud, ImageID: image, Libvirt: &compute.AdministrationLibvirtImage{Hosts: []compute.AdministrationImageHost{}}}
	if b.Images == nil {
		return out, compute.ErrUnavailable
	}
	i, err := b.Images.ForCloud(cloud).Get(image)
	if err != nil {
		return out, err
	}
	for _, h := range hosts {
		entry := compute.AdministrationImageHost{HostID: h.ID, Observation: "unknown", Compatibility: "unknown", SourceState: "unknown"}
		if _, err := b.Host(ctx, cloud, h.ID); err != nil {
			entry.Observation = "unavailable"
			entry.Code = "host_unavailable"
			out.Libvirt.Hosts = append(out.Libvirt.Hosts, entry)
			continue
		}
		caps, err := b.Capabilities(ctx, cloud, h.ID, i.Arch)
		if err != nil {
			entry.Observation = "unavailable"
			entry.Code = "capabilities_unavailable"
			out.Libvirt.Hosts = append(out.Libvirt.Hosts, entry)
			continue
		}
		entry.Observation = "observed"
		entry.Compatibility = "unsupported"
		if i.Compatible(caps) {
			entry.Compatibility = "supported"
		}
		if b.Volumes == nil {
			entry.Observation = "unavailable"
			entry.Code = "storage_unavailable"
		} else if !i.Managed() {
			v, e := b.Volumes.Get(i.SourcePath, h.ID)
			switch {
			case errors.Is(e, compute.ErrNotFound) || errors.Is(e, compute.ErrVolumeNotFound):
				entry.SourceState = "missing"
			case e != nil:
				entry.Observation = "unavailable"
				entry.Code = "storage_unavailable"
			case v.Metadata.Hidden:
				entry.SourceState = "hidden"
			case v.AttachedTo != "":
				entry.SourceState = "attached"
			default:
				entry.SourceState = "ready"
			}
			if v != nil {
				entry.VolumePath = v.Path
				entry.Pool = v.Pool
			}
		} else if b.Images.ManagedSources == nil {
			entry.Observation = "unavailable"
			entry.Code = "image_storage_unavailable"
		} else {
			binding, e := b.Images.ManagedSources.Bind(h.ID, i)
			if e != nil {
				entry.Observation = "unavailable"
				entry.Code = "image_storage_unavailable"
			} else {
				state, _ := b.Images.ManagedSources.Status(binding, i)
				switch state {
				case "ready", "downloadable", "preparing", "failed":
					entry.SourceState = state
				default:
					entry.SourceState = "unknown"
				}
				entry.VolumePath = binding.Path
				entry.Pool = binding.Pool
				// Status verifies the accepted artifact record. Independently observe
				// storage so a failed connection is unknown, and an attached or
				// vanished source cannot be presented as ready.
				volume, inspectErr := b.Volumes.Get(binding.Path, h.ID)
				switch {
				case errors.Is(inspectErr, compute.ErrNotFound) || errors.Is(inspectErr, compute.ErrVolumeNotFound):
					if state != "downloadable" && state != "preparing" {
						entry.SourceState = "missing"
					}
				case inspectErr != nil:
					entry.Observation = "unavailable"
					entry.SourceState = "unknown"
					entry.Code = "storage_unavailable"
				case volume.AttachedTo != "":
					entry.SourceState = "attached"
				}
				// Managed cache volumes are hidden by construction; that internal
				// protection is intentionally exempt from local-source visibility,
				// matching PrepareImages rather than changing downloaded policy.
			}
		}
		out.Libvirt.Hosts = append(out.Libvirt.Hosts, entry)
	}
	return out, nil
}

var _ compute.AdministrationObservations = (*AdministrationObservations)(nil)
