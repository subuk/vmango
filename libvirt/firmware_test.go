package libvirt

import (
	"encoding/json"
	"github.com/rs/zerolog"
	vir "libvirt.org/go/libvirt"
	x "libvirt.org/go/libvirtxml"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/libvirt/hardware"
	"testing"
)

func testFirmwareBinding() hardware.FirmwareBinding {
	return hardware.FirmwareBinding{Type: "efi", Loader: "/firmware/v1/CODE", LoaderType: "pflash", LoaderFormat: "raw", Readonly: "yes", Secure: "yes", NVRAMTemplate: "/firmware/v1/VARS", NVRAMFormat: "raw", NVRAM: "/session/nvram/private", SecureBoot: boolPointer(true), EnrolledKeys: boolPointer(true), Owned: true, OwnerVM: "vm-id", Namespace: "ns"}
}
func TestFirmwareXMLAndOwnership(t *testing.T) {
	b := testFirmwareBinding()
	c := &x.Domain{UUID: "vm-id", OS: &x.DomainOS{}}
	applyFirmware(c, &b)
	raw, _ := c.Marshal()
	for _, want := range []string{`readonly="yes"`, `type="pflash"`, `template="/firmware/v1/VARS"`, `/session/nvram/private`} {
		if !strings.Contains(raw, want) {
			t.Fatalf("missing %s: %s", want, raw)
		}
	}
	if strings.Contains(raw, `<firmware>`) || strings.Contains(raw, `firmware="efi"`) || strings.Contains(raw, `firmware="auto"`) || strings.Contains(raw, `format="raw"`) {
		t.Fatal("invalid auto or unsupported old-host format attribute", raw)
	}
	if err := supportedFirmwareXML(c); err != nil {
		t.Fatal(err)
	}
	if !sameFirmware(b, observeFirmware(c)) {
		t.Fatalf("observation mismatch %+v", observeFirmware(c))
	}
	plan := hardware.Plan{Firmware: &b}
	if firmwareUndefineFlags(c, plan) != vir.DOMAIN_UNDEFINE_NVRAM {
		t.Fatal("owned NVRAM not removed")
	}
	b.Owned = false
	if firmwareUndefineFlags(c, plan) != vir.DOMAIN_UNDEFINE_KEEP_NVRAM {
		t.Fatal("external NVRAM not preserved")
	}
	b.Owned = true
	b.OwnerVM = "other"
	if firmwareUndefineFlags(c, plan) != vir.DOMAIN_UNDEFINE_KEEP_NVRAM {
		t.Fatal("foreign ownership accepted")
	}
	b.OwnerVM = "vm-id"
	b.NVRAM = ""
	if firmwareUndefineFlags(c, plan) != vir.DOMAIN_UNDEFINE_KEEP_NVRAM {
		t.Fatal("interrupted unconfirmed allocation deleted")
	}
	b = testFirmwareBinding()
	b.LoaderFormat = "qcow2"
	b.NVRAMFormat = "qcow2"
	applyFirmware(c, &b)
	raw, _ = c.Marshal()
	if !strings.Contains(raw, `templateFormat="qcow2"`) {
		t.Fatal(raw)
	}
}
func TestFirmwareSelectionAndDrift(t *testing.T) {
	c := &x.Domain{OS: &x.DomainOS{}}
	firmwareRequirements(c.OS, hardware.Firmware{Type: "auto"})
	if c.OS.Firmware != "" {
		t.Fatal("emitted firmware auto")
	}
	firmwareRequirements(c.OS, hardware.Firmware{Type: "efi", SecureBoot: boolPointer(false), EnrolledKeys: boolPointer(false)})
	if len(c.OS.FirmwareInfo.Features) != 1 {
		t.Fatal("redundant enrolled-keys filter emitted")
	}
	b := testFirmwareBinding()
	applyFirmware(c, &b)
	before, _ := c.Marshal()
	p := hardware.Profile{ID: "existing", FirmwarePolicy: &hardware.Firmware{Type: "auto"}}
	if err := validateFirmwareAdoption(c, p, nil); err != nil {
		t.Fatal(err)
	}
	after, _ := c.Marshal()
	if before != after {
		t.Fatal("adoption rewrote firmware")
	}
	if err := firmwareDrift(&b, observeFirmware(c)); err != nil {
		t.Fatal(err)
	}
	c.OS.NVRam.Template = "/different"
	if err := firmwareDrift(&b, observeFirmware(c)); err == nil {
		t.Fatal("template drift accepted")
	}
	c.OS.VarStore = &x.DomainVarStore{}
	if firmwareDrift(&b, observeFirmware(c)) == nil {
		t.Fatal("varstore drift ignored")
	}
	if supportedFirmwareXML(c) == nil {
		t.Fatal("varstore accepted")
	}
	c.OS.VarStore = nil
	c.OS.Loader.Stateless = "yes"
	if supportedFirmwareXML(c) == nil {
		t.Fatal("stateless accepted")
	}
}
func TestFirmwarePersistenceAndUnrelatedResources(t *testing.T) {
	b := testFirmwareBinding()
	p, err := hardware.Resolve(hardware.Profile{ID: "stable"}, "/qemu")
	if err != nil {
		t.Fatal(err)
	}
	p.Firmware = &b
	raw, _ := json.Marshal(p)
	restored, err := hardware.DecodePlan(raw)
	if err != nil || !reflect.DeepEqual(restored.Firmware, &b) {
		t.Fatal(err, restored)
	}
	c := &x.Domain{UUID: "vm-id", OS: &x.DomainOS{}}
	applyFirmware(c, &b)
	before := *c.OS
	applyDomainPolicy(c, &compute.VirtualMachine{VCpus: 4, Memory: compute.NewSize(1, compute.SizeUnitG)}, &x.Caps{}, true)
	if !reflect.DeepEqual(*c.OS, before) {
		t.Fatal("resource change rewrote firmware")
	}
	vm := &compute.VirtualMachine{Id: "vm-id", NodeId: "host", AppliedPolicy: raw}
	refs := (&VirtualMachineRepository{}).BackendArtifactRefs(vm)
	if len(refs) != 1 || refs[0].ID != b.NVRAM {
		t.Fatal("actual artifact missing from task effects", refs)
	}
}

// Opt-in read-only capability validation: no domains, pools or files are created.
func TestFirmwareRealPreflight(t *testing.T) {
	uri := os.Getenv("FIRMWARE_TEST_URI")
	if uri == "" {
		t.Skip("read-only real host check is opt-in")
	}
	pool := NewConnectionPool(map[string]string{"host": uri}, []string{"host"}, zerolog.Nop())
	repo := NewVirtualMachineRepository(pool, map[string]NodeSettings{"host": {}}, zerolog.Nop())
	conn, err := pool.Acquire("host")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Release("host")
	arch, machine, domain := os.Getenv("FIRMWARE_TEST_ARCH"), os.Getenv("FIRMWARE_TEST_MACHINE"), os.Getenv("FIRMWARE_TEST_DOMAIN")
	caps, err := queryDomainCapabilities(conn)("", arch, machine, domain)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"auto", "bios", "efi", "secure"} {
		t.Run(mode, func(t *testing.T) {
			f := hardware.Firmware{Type: mode}
			if mode == "secure" {
				f.Type = "efi"
				f.SecureBoot = boolPointer(true)
			}
			f, err = hardware.NormalizeFirmware(f, false)
			if err != nil {
				t.Fatal(err)
			}
			b, err := repo.resolveFirmware(conn, hardware.Profile{ID: "test", Firmware: f.Type, FirmwarePolicy: &f}, nil, caps, "host")
			if err != nil {
				t.Logf("unsupported %s: %v", mode, err)
				if mode == os.Getenv("FIRMWARE_TEST_REQUIRED") {
					t.Fatal(err)
				}
				return
			}
			t.Logf("resolved %+v", b)
		})
	}
}

func TestFirmwareAcceptedHostBindingsDoNotFollowCatalogEdits(t *testing.T) {
	if binding, err := (&VirtualMachineRepository{}).resolveFirmware(nil, hardware.Profile{Firmware: "efi"}, nil, nil, "old-host"); err != nil || binding != nil {
		t.Fatal("historical accepted policy received new firmware defaults", binding, err)
	}
	repo := &VirtualMachineRepository{settings: map[string]NodeSettings{"h": {FirmwareProfiles: []hardware.FirmwareHostBinding{{ID: "v1", Loader: "/v1/CODE", NVRAMTemplate: "/v1/VARS"}}}}}
	raw, err := repo.SnapshotCreationBinding("h")
	if err != nil {
		t.Fatal(err)
	}
	repo.settings["h"].FirmwareProfiles[0].Loader = "/v2/CODE"
	var restored creationBinding
	if err = json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.FirmwareProfiles[0].Loader != "/v1/CODE" {
		t.Fatal("accepted binding follows mutable catalog")
	}
	b := testFirmwareBinding()
	b.OwnerVM = ""
	b.NVRAM = ""
	p, _ := hardware.Resolve(hardware.Profile{ID: "p"}, "")
	p.Firmware = &b
	plan, _ := json.Marshal(p)
	vm := &compute.VirtualMachine{Id: "actual-uuid", NamespaceID: "team"}
	if err := repo.ApplyHardware(vm, plan); err != nil {
		t.Fatal(err)
	}
	applied, err := hardware.DecodePlan(vm.AppliedPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if applied.Firmware.OwnerVM != "actual-uuid" || applied.Firmware.Namespace != "team" {
		t.Fatal("allocation intent was not bound to assigned VM/namespace", applied.Firmware)
	}
}

func TestFirmwareHostFilesFailBeforeProvisioning(t *testing.T) {
	dir := t.TempDir()
	code, vars := filepath.Join(dir, "CODE"), filepath.Join(dir, "VARS")
	for _, name := range []string{code, vars} {
		if err := os.WriteFile(name, []byte("raw-firmware"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	b := hardware.FirmwareBinding{Loader: code, LoaderFormat: "raw", NVRAMTemplate: vars, NVRAMFormat: "raw"}
	if err := validateFirmwareFiles("qemu:///session", b); err != nil {
		t.Fatal(err)
	}
	b.NVRAMFormat = "qcow2"
	if validateFirmwareFiles("qemu:///session", b) == nil {
		t.Fatal("wrong format accepted")
	}
	b.NVRAMFormat = "raw"
	if err := os.WriteFile(vars, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if validateFirmwareFiles("qemu:///session", b) == nil {
		t.Fatal("empty template accepted")
	}
	if err := os.Remove(vars); err != nil {
		t.Fatal(err)
	}
	if validateFirmwareFiles("qemu:///session", b) == nil {
		t.Fatal("missing template accepted")
	}
}
