//go:build acceptance

package acceptance

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"libvirt.org/go/libvirtxml"
	"os"
	"path/filepath"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/tasks"
	"testing"
	"time"
)

// testStorageCloning uses API-created volumes in the run's isolated pool.
// It can run without a VM on hosts whose workload admission is exhausted.
func testStorageCloning(t *testing.T, root, pool, machine string,
	request func(string, string, any, int) []byte,
	command func(string, string, any) []byte,
	vir func(...string) string,
	run func(string, ...string) (string, error),
) {
	t.Helper()
	const route = "/api/v1/clouds/acceptance/volumes"
	decode := func(data []byte) (string, string) {
		t.Helper()
		var v struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(data, &v); err != nil {
			t.Fatal(err)
		}
		address, err := compute.ParseVolumeID(v.ID)
		if err != nil || address.Host != "acceptance" {
			t.Fatal("invalid storage-test volume address", v.ID, err)
		}
		return v.ID, address.Path
	}
	metadata := func(path string) libvirtxml.StorageVolume {
		t.Helper()
		var v libvirtxml.StorageVolume
		if err := v.Unmarshal(vir("vol-dumpxml", "--pool", pool, path)); err != nil {
			t.Fatal(err)
		}
		return v
	}
	reject := func(method, path string, value any) {
		t.Helper()
		body := request(method, path, value, 0)
		var accepted struct {
			StatusURL string `json:"status_url"`
		}
		if err := json.Unmarshal(body, &accepted); err != nil {
			t.Fatal(err)
		}
		if accepted.StatusURL == "" {
			if !strings.Contains(string(body), "dependent copy") {
				t.Fatalf("unexpected rejection reason: %s", body)
			}
			return
		}
		for deadline := time.Now().Add(time.Minute); time.Now().Before(deadline); {
			var task tasks.Task
			if err := json.Unmarshal(request("GET", accepted.StatusURL, nil, 200), &task); err != nil {
				t.Fatal(err)
			}
			if task.State.Terminal() {
				if task.State != tasks.Failed || len(task.Effects) != 0 || task.OutcomeUncertain || task.Error == nil || !strings.Contains(task.Error.Message, "dependent copy") {
					t.Fatalf("unsafe rejection result: %+v", task)
				}
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatal("rejected operation did not finish")
	}
	download := func(path, label string) string {
		t.Helper()
		target := filepath.Join(root, label)
		vir("vol-download", "--pool", pool, path, target)
		return target
	}
	for _, format := range []string{"raw", "qcow2"} {
		t.Logf("checking %s storage cloning", format)
		func() {
			class := "standard"
			if format == "raw" {
				class = "raw"
			}
			id, base := decode(command("POST", route, map[string]any{"name": "clone-base-" + format, "zone": "test", "storage_class": class, "size_bytes": 16 << 20}))
			// Seed nonzero data before dependencies exist. External upload is confined
			// to this newly created, unattached test volume.
			seed := filepath.Join(root, "seed-"+format+".raw")
			data := make([]byte, 16<<20)
			copy(data[1<<20:], []byte("vmango overlay independent-copy acceptance marker"))
			if err := os.WriteFile(seed, data, 0600); err != nil {
				t.Fatal(err)
			}
			upload := seed
			if format == "qcow2" {
				upload = filepath.Join(root, "seed.qcow2")
				if out, err := run("qemu-img", "convert", "-f", "raw", "-O", "qcow2", seed, upload); err != nil {
					t.Fatal(out, err)
				}
			}
			vir("vol-upload", "--pool", pool, base, upload)
			vir("pool-refresh", pool)
			before, err := os.ReadFile(download(base, "base-before-"+format))
			if err != nil {
				t.Fatal(err)
			}
			var children, childPaths []string
			for i := 0; i < 2; i++ {
				childID, path := decode(command("POST", route+"/"+id+"/clone", map[string]any{"name": fmt.Sprintf("child-%s-%d", format, i), "storage_class": "efficient", "size_bytes": 32 << 20}))
				children = append(children, childID)
				childPaths = append(childPaths, path)
				v := metadata(path)
				if v.Target == nil || v.Target.Format == nil || v.Target.Format.Type != "qcow2" || v.Capacity == nil || v.Capacity.Value != 32<<20 || v.BackingStore == nil || v.BackingStore.Path != base || v.BackingStore.Format == nil || v.BackingStore.Format.Type != format {
					t.Fatalf("incorrect dependent clone metadata: %+v", v)
				}
			}
			// Put a child-only change into the first isolated overlay. Construct it
			// against a matching local base, then change only its backing reference
			// before upload. This never rebases or writes any existing user disk.
			expected := make([]byte, 32<<20)
			copy(expected, data)
			copy(expected[2<<20:], []byte("vmango child-only changed block"))
			expectedPath := filepath.Join(root, "expected-child-"+format+".raw")
			if err := os.WriteFile(expectedPath, expected, 0600); err != nil {
				t.Fatal(err)
			}
			childUpload := filepath.Join(root, "changed-child-"+format+".qcow2")
			if out, err := run("qemu-img", "convert", "-f", "raw", "-O", "qcow2", "-B", upload, "-F", format, expectedPath, childUpload); err != nil {
				t.Fatal(out, err)
			}
			if out, err := run("qemu-img", "rebase", "-u", "-f", "qcow2", "-F", format, "-b", base, childUpload); err != nil {
				t.Fatal(out, err)
			}
			vir("vol-upload", "--pool", pool, childPaths[0], childUpload)
			vir("pool-refresh", pool)
			reject("POST", route+"/"+children[0]+"/clone", map[string]any{"name": "forbidden-nested-" + format, "storage_class": "efficient", "size_bytes": 32 << 20})
			reject("POST", route+"/"+id+"/resize", map[string]any{"size_bytes": 32 << 20})
			reject("DELETE", route+"/"+id, nil)
			if machine != "" {
				command("POST", machine+"/disks/attach", map[string]any{"volume_id": id})
				reject("POST", machine+"/actions", map[string]string{"action": "start"})
				command("DELETE", machine+"/disks/"+id, nil)
			}
			copyID, copyPath := decode(command("POST", route+"/"+children[0]+"/clone", map[string]any{"name": "independent-" + format, "storage_class": "standard", "size_bytes": 32 << 20}))
			v := metadata(copyPath)
			if v.BackingStore != nil && v.BackingStore.Path != "" {
				t.Fatal("independent clone retained a backing store")
			}
			for _, child := range children {
				command("DELETE", route+"/"+child, nil)
			}
			after, err := os.ReadFile(download(base, "base-after-"+format))
			if err != nil || sha256.Sum256(before) != sha256.Sum256(after) {
				t.Fatal("base contents changed", err)
			}
			command("DELETE", route+"/"+id, nil)
			independent := download(copyPath, "independent-"+format+".qcow2")
			contents := filepath.Join(root, "independent-"+format+".raw")
			if out, err := run("qemu-img", "convert", "-f", "qcow2", "-O", "raw", independent, contents); err != nil {
				t.Fatal("copy cannot be read after deleting source chain", out, err)
			}
			actual, err := os.ReadFile(contents)
			if err != nil || !bytes.Equal(actual, expected) {
				t.Fatal("independent copy lost base or child-only contents", err)
			}
			command("DELETE", route+"/"+copyID, nil)
			t.Logf("verified %s base, two larger overlays, mutation protection, unchanged base, and independent copy with child-only changes after source deletion", format)
			if machine != "" {
				t.Log("verified referenced base cannot start writable")
			}
		}()
	}
}
