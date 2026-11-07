package installer

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"subuk/vmango/compute"
)

type resourceObserver interface {
	Observe(context.Context, string, compute.HostPreparationSpec) (compute.HostPreparationState, error)
}

// Discover explicitly replaces selected desired resource fields with the actual
// named resources' settings. It never prepares or applies backend operations.
func (c *Controller) Discover(revision uint64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.check(revision); err != nil {
		return err
	}
	hosts := c.selectedLocked()
	if len(hosts) == 0 {
		return fmt.Errorf("select hosts before loading existing resource settings")
	}
	c.data.State.Plan = nil
	c.data.State.Phase = "settings"
	c.data.Approved = false
	c.data.RichtPlan = nil
	c.data.ResourcePlans = nil
	c.data.ConfigDigest = ""
	c.data.ResourceDigest = ""
	c.candidate = nil
	return c.startLocked(func(ctx context.Context) error {
		observer, ok := c.opts.Resources(hosts).(resourceObserver)
		if !ok {
			return fmt.Errorf("existing resource inspection is unavailable for this backend")
		}
		var wg sync.WaitGroup
		var mu sync.Mutex
		changed := false
		failures := 0
		semaphore := make(chan struct{}, c.opts.Parallelism)
		for _, host := range hosts {
			host := host
			wg.Add(1)
			go func() {
				defer wg.Done()
				select {
				case semaphore <- struct{}{}:
				case <-ctx.Done():
					mu.Lock()
					failures++
					mu.Unlock()
					return
				}
				defer func() { <-semaphore }()
				c.result(host.ID, "observing", "Reading existing libvirt network and pool settings")
				probe, cancel := context.WithTimeout(ctx, 90*time.Second)
				defer cancel()
				observed, err := observer.Observe(probe, host.ID, resourceSpec(host))
				if err == nil {
					err = probe.Err()
				}
				next := host
				message, status := "", "observed"
				if err != nil {
					message = "Cannot read existing resources: " + err.Error() + ". Check the libvirt daemon and runtime access, or prepare the host first."
					status = "failed"
				} else {
					var messages []string
					if !observed.Network.Exists {
						messages = append(messages, "Network "+host.Network+" is absent; desired settings are unchanged.")
					} else if observed.Network.Mode != "nat" || !observed.Network.ShapeCompatible {
						messages = append(messages, "Existing network "+host.Network+" is not a supported single-subnet NAT network; desired settings are unchanged.")
						status = "failed"
					} else {
						next.Subnet = observed.Network.Subnet
						next.DHCPStart = observed.Network.DHCPStart
						next.DHCPEnd = observed.Network.DHCPEnd
						messages = append(messages, "Loaded existing NAT network "+host.Network+" settings.")
					}
					if !observed.Pool.Exists {
						messages = append(messages, "Pool "+host.Pool+" is absent; desired settings are unchanged.")
					} else if observed.Pool.Type != "dir" {
						messages = append(messages, "Existing pool "+host.Pool+" is not a directory pool; desired settings are unchanged.")
						status = "failed"
					} else {
						next.PoolPath = observed.Pool.Path
						messages = append(messages, "Loaded existing directory pool "+host.Pool+" path.")
					}
					if err := compute.ValidateHostPreparationSpec(resourceSpec(next)); err != nil {
						next = host
						status = "failed"
						messages = []string{"Existing resource settings cannot be represented by this installer: " + err.Error() + ". Desired settings are unchanged."}
					}
					message = strings.Join(messages, " ") + " No resources were changed."
				}
				c.mu.Lock()
				for i := range c.data.State.Hosts {
					if c.data.State.Hosts[i].ID != host.ID {
						continue
					}
					if !EqualAnswers(host, next) {
						mu.Lock()
						changed = true
						mu.Unlock()
						delete(c.data.HostReady, host.ID)
						delete(c.data.ResourceReady, host.ID)
						delete(c.data.ReadyPlans, host.ID)
					}
					next.Status, next.Message = status, message
					c.data.State.Hosts[i] = next
					break
				}
				c.mu.Unlock()
				if status == "failed" {
					mu.Lock()
					failures++
					mu.Unlock()
				}
				c.result(host.ID, status, message)
			}()
		}
		wg.Wait()
		if changed {
			c.mu.Lock()
			c.data.State.Revision++
			c.mu.Unlock()
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if failures > 0 {
			return fmt.Errorf("existing resource settings could not be loaded for %d host(s); review their individual results", failures)
		}
		return nil
	})
}
