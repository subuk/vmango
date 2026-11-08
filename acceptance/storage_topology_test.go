//go:build acceptance

package acceptance

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"subuk/vmango/tasks"
	"testing"
	"time"

	x "libvirt.org/go/libvirtxml"
)

func testStorageTopology(t *testing.T, id, path, bus, pool string,
	request func(string, string, any, int) []byte,
	command func(string, string, any) []byte,
	vir func(...string) string) {
	t.Helper()
	read := func() *x.Domain {
		c := &x.Domain{}
		if e := c.Unmarshal(vir("dumpxml", id, "--inactive")); e != nil {
			t.Fatal(e)
		}
		return c
	}
	before := read()
	// Active topology changes must be rejected at request preparation.
	request("POST", path+"/disks", map[string]any{"name": "running-rejected", "storage_class": "standard", "size_bytes": 16 << 20}, 409)
	command("POST", path+"/actions", map[string]string{"action": "poweroff"})
	var volume struct {
		ID string `json:"id"`
	}
	if e := json.Unmarshal(command("POST", "/api/v1/clouds/acceptance/volumes", map[string]any{"name": "topology-data", "zone": "test", "storage_class": "standard", "size_bytes": 16 << 20}), &volume); e != nil {
		t.Fatal(e)
	}
	command("POST", path+"/disks/attach", map[string]any{"volume_id": volume.ID, "alias": "topology-data"})
	attached := read()
	if len(attached.Devices.Controllers) != len(before.Devices.Controllers) {
		t.Fatal("attach expanded controllers")
	}
	for i, disk := range before.Devices.Disks {
		if !reflect.DeepEqual(disk, attached.Devices.Disks[i]) {
			t.Fatal("attach changed existing disk")
		}
	}
	added := attached.Devices.Disks[len(attached.Devices.Disks)-1]
	if added.Target == nil || added.Target.Bus != bus || added.Address == nil {
		t.Fatal("attachment lost bus/address", added)
	}
	command("DELETE", path+"/disks/"+volume.ID, nil)
	detached := read()
	if !reflect.DeepEqual(detached.Devices.Controllers, attached.Devices.Controllers) || !reflect.DeepEqual(detached.Devices.Disks, before.Devices.Disks) {
		t.Fatal("detach relocated hardware or removed controller")
	}
	command("POST", path+"/disks/attach", map[string]any{"volume_id": volume.ID, "alias": "topology-data"})
	reattached := read()
	last := reattached.Devices.Disks[len(reattached.Devices.Disks)-1]
	if !reflect.DeepEqual(last.Address, added.Address) {
		t.Fatal("reattach did not reuse freed address")
	}
	if bus == "sata" {
		portsUsed := func() int {
			count := 0
			for _, disk := range read().Devices.Disks {
				if disk.Target != nil && disk.Target.Bus == "sata" {
					count++
				}
			}
			return count
		}
		for portsUsed() < 6 {
			command("POST", path+"/disks", map[string]any{"name": fmt.Sprintf("capacity-%d", len(read().Devices.Disks)), "storage_class": "standard", "size_bytes": 16 << 20})
		}
		full := read()
		volumesBefore := vir("vol-list", "--pool", pool)
		raw := request("POST", path+"/disks", map[string]any{"name": "capacity-rejected", "storage_class": "standard", "size_bytes": 16 << 20}, 202)
		var accepted struct {
			StatusURL string `json:"status_url"`
		}
		if err := json.Unmarshal(raw, &accepted); err != nil || accepted.StatusURL == "" {
			t.Fatal("invalid capacity task response", string(raw))
		}
		var rejected tasks.Task
		deadline := time.Now().Add(time.Minute)
		for time.Now().Before(deadline) {
			if err := json.Unmarshal(request("GET", accepted.StatusURL, nil, 200), &rejected); err != nil {
				t.Fatal(err)
			}
			if rejected.State.Terminal() {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if rejected.State != tasks.Failed || rejected.OutcomeUncertain || len(rejected.Effects) != 0 || rejected.Error == nil || !strings.Contains(rejected.Error.Message, "capacity 6") {
			t.Fatalf("capacity task did not fail without effects: %+v", rejected)
		}
		if !reflect.DeepEqual(full, read()) || volumesBefore != vir("vol-list", "--pool", pool) {
			t.Fatal("capacity rejection changed domain or created a volume")
		}
		reattached = full
		t.Log("SATA six-port boundary rejected another disk before volume/domain mutation")
	}
	command("POST", path+"/actions", map[string]string{"action": "start"})
	running := read()
	for i, disk := range reattached.Devices.Disks {
		if !reflect.DeepEqual(disk.Target, running.Devices.Disks[i].Target) || !reflect.DeepEqual(disk.Address, running.Devices.Disks[i].Address) {
			t.Fatal("cold start changed disk address")
		}
	}
	t.Log(fmt.Sprintf("verified %s stopped attach/detach/reattach, unchanged existing targets/addresses/controllers, active rejection and restart persistence", bus))
}
