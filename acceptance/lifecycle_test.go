//go:build acceptance

package acceptance

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	encodingxml "encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/tasks"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/ssh"
	"libvirt.org/go/libvirtxml"
)

// TestLifecycle boots a real guest. Explicit opt-in and a local libvirt URI are
// required; this suite must never run as part of ordinary unit tests.
func TestLifecycle(t *testing.T) {
	image := os.Getenv("ACCEPTANCE_IMAGE")
	imageURL := os.Getenv("ACCEPTANCE_IMAGE_URL")
	imageSHA := os.Getenv("ACCEPTANCE_IMAGE_SHA256")
	var sourceBytes uint64
	if value := os.Getenv("ACCEPTANCE_IMAGE_SIZE_BYTES"); value != "" {
		var err error
		sourceBytes, err = strconv.ParseUint(value, 10, 64)
		if err != nil || sourceBytes == 0 {
			t.Fatal("ACCEPTANCE_IMAGE_SIZE_BYTES must be a positive byte count")
		}
	}

	if image == "" && imageURL == "" {
		t.Fatal("set ACCEPTANCE_IMAGE to a local Ubuntu cloud image (see README.md)")
	}
	image, err := filepath.Abs(image)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(image); imageURL == "" && err != nil {
		t.Fatal(err)
	}
	uri := os.Getenv("ACCEPTANCE_URI")
	if uri == "" {
		uri = "qemu:///session"
	}
	remote := os.Getenv("ACCEPTANCE_REMOTE") == "1"
	if remote && (!strings.HasPrefix(uri, "qemu+ssh://") || imageURL == "") {
		t.Fatal("remote acceptance requires qemu+ssh URI and downloadable image")
	}
	if !remote && uri != "qemu:///session" && uri != "qemu:///system" {
		t.Fatal("only local qemu:///session or qemu:///system is supported")
	}
	arch := "x86_64"
	if runtime.GOARCH == "arm64" {
		arch = "aarch64"
	}
	if remote {
		arch = os.Getenv("ACCEPTANCE_ARCH")
		if arch != "x86_64" && arch != "aarch64" {
			t.Fatal("remote acceptance needs ACCEPTANCE_ARCH")
		}
	}
	adoptPower := os.Getenv("ACCEPTANCE_ADOPT_POWER") == "1"
	if adoptPower && arch != "x86_64" {
		t.Fatal("ACCEPTANCE_ADOPT_POWER requires an x86_64 guest")
	}
	expectedType := "kvm"
	configDriveBus := os.Getenv("ACCEPTANCE_CONFIG_DRIVE_BUS")
	if configDriveBus == "" {
		configDriveBus = "virtio"
	}
	configDriveDevice := os.Getenv("ACCEPTANCE_CONFIG_DRIVE_DEVICE")
	if configDriveDevice == "" {
		configDriveDevice = "disk"
	}
	if configDriveBus != "virtio" && configDriveBus != "scsi" && configDriveBus != "ide" {
		t.Fatal("invalid ACCEPTANCE_CONFIG_DRIVE_BUS")
	}
	if configDriveDevice != "disk" && configDriveDevice != "cdrom" {
		t.Fatal("invalid ACCEPTANCE_CONFIG_DRIVE_DEVICE")
	}
	firmware := "auto"
	initialization := os.Getenv("ACCEPTANCE_INITIALIZATION")
	if initialization == "" {
		initialization = "nocloud"
	}
	if initialization != "nocloud" && initialization != "openstack" {
		t.Fatal("ACCEPTANCE_INITIALIZATION must be nocloud or openstack")
	}
	if runtime.GOOS == "darwin" && !remote {
		expectedType = "hvf"
		firmware = "efi"
	}
	if remote && arch == "aarch64" {
		firmware = "efi"
	}
	expectedType, bootTimeout, err := acceptanceRuntimeOptions(expectedType, os.Getenv("ACCEPTANCE_VIRT_TYPE"), os.Getenv("ACCEPTANCE_BOOT_TIMEOUT"))
	if err != nil {
		t.Fatal(err)
	}
	if imageURL != "" && len(imageSHA) != 64 {
		t.Fatal("ACCEPTANCE_IMAGE_SHA256 is required with ACCEPTANCE_IMAGE_URL")
	}
	name := "vmango-acceptance-" + uuid.NewString()
	root, err := filepath.Abs("../result/" + name)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(root), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	t.Logf("artifacts: %s", root)
	run := func(command string, args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, command, args...)
		cmd.Env = append(os.Environ(), "LC_ALL=C")
		b, err := cmd.CombinedOutput()
		return string(b), err
	}
	vir := func(args ...string) (string, error) { return run("virsh", append([]string{"-c", uri}, args...)...) }
	mustVir := func(args ...string) string {
		t.Helper()
		out, err := vir(args...)
		if err != nil {
			t.Fatalf("virsh %v: %v\n%s", args, err, out)
		}
		return out
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	mustVir("list", "--all")
	if _, err := vir("dominfo", name); err == nil {
		t.Fatal("unexpected existing test domain")
	}
	poolDir := filepath.Join(root, "pool")
	if remote {
		poolDir = "/tmp/" + name
	}
	if !remote {
		if err := os.Mkdir(poolDir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	// Pool creation fails on collisions. Cleanup is limited to this random name.
	mustVir("pool-define-as", name, "dir", "--target", poolDir)
	if remote {
		mustVir("pool-build", name)
	}
	poolUUID := strings.TrimSpace(mustVir("pool-uuid", name))
	domainUUID := ""
	lastCreatedVM := ""
	ownedDomains := map[string]bool{}
	write(filepath.Join(root, "resources.txt"), "pool="+name+"\npool_uuid="+poolUUID+"\npool_path="+poolDir+"\n")
	t.Cleanup(func() {
		// A connection error is not evidence that the domain is gone. Never
		// remove its storage unless domain absence/undefine was confirmed.
		domains, err := vir("list", "--all", "--uuid")
		if err != nil {
			t.Errorf("cleanup cannot list domains: %s %v", domains, err)
			return
		}
		for domain := range ownedDomains {
			exists := false
			for _, candidate := range strings.Fields(domains) {
				if candidate == domain {
					exists = true
				}
			}
			if exists {
				xml, err := vir("dumpxml", domain)
				if err != nil {
					t.Errorf("cleanup cannot inspect domain: %s %v", xml, err)
					return
				}
				if err := os.WriteFile(filepath.Join(root, "domain.xml"), []byte(xml), 0600); err != nil {
					t.Error(err)
				}
				var ownership struct {
					UUID  string `xml:"uuid"`
					Disks []struct {
						Source struct {
							File string `xml:"file,attr"`
							Dev  string `xml:"dev,attr"`
						} `xml:"source"`
					} `xml:"devices>disk"`
				}
				if err := encodingxml.Unmarshal([]byte(xml), &ownership); err != nil || len(ownership.Disks) == 0 || domain != ownership.UUID {
					t.Errorf("refusing cleanup: cannot verify test domain ownership")
					return
				}
				for _, disk := range ownership.Disks {
					if disk.Source.Dev != "" || !strings.HasPrefix(disk.Source.File, poolDir+string(os.PathSeparator)) {
						t.Errorf("refusing cleanup: domain has storage outside the test pool")
						return
					}
				}
				state, err := vir("domstate", domain)
				if err != nil {
					t.Errorf("cleanup cannot get domain state: %v", err)
					return
				}
				if strings.TrimSpace(state) != "shut off" {
					if out, err := vir("destroy", domain); err != nil {
						t.Errorf("cleanup cannot stop domain: %s %v", out, err)
						return
					}
				}
				if out, err := vir("undefine", domain, "--nvram"); err != nil {
					t.Errorf("cleanup domain: %s %v", out, err)
					return
				}
			}
		}
		currentUUID, err := vir("pool-uuid", name)
		if err != nil || strings.TrimSpace(currentUUID) != poolUUID {
			t.Errorf("refusing cleanup: pool identity changed")
			return
		}
		if remote {
			listing, err := vir("vol-list", "--pool", name)
			if err != nil {
				t.Errorf("cannot list owned pool volumes: %s %v", listing, err)
				return
			}
			for _, line := range strings.Split(listing, "\n")[2:] {
				fields := strings.Fields(line)
				if len(fields) == 0 {
					continue
				}
				volume := fields[0]
				volPath, err := vir("vol-path", "--pool", name, volume)
				if err != nil || !strings.HasPrefix(strings.TrimSpace(volPath), poolDir+"/") {
					t.Error("refusing cleanup outside owned pool")
					return
				}
				if out, err := vir("vol-delete", "--pool", name, volume); err != nil {
					t.Errorf("cleanup owned volume: %s %v", out, err)
					return
				}
			}
		}
		vir("pool-destroy", name)
		if remote {
			if out, err := vir("pool-delete", name); err != nil {
				t.Errorf("cleanup remote pool directory: %s %v", out, err)
				return
			}
		}
		if out, err := vir("pool-undefine", name); err != nil {
			t.Errorf("cleanup pool: %s %v", out, err)
			return
		}
		if err := func() error {
			if remote {
				return nil
			}
			return os.RemoveAll(poolDir)
		}(); err != nil {
			t.Error(err)
		}
	})
	mustVir("pool-start", name)
	base := filepath.Join(poolDir, "base.qcow2")
	if imageURL == "" {
		if out, err := run("qemu-img", "convert", "-O", "qcow2", image, base); err != nil {
			t.Fatalf("prepare image: %s %v", out, err)
		}
	}
	if imageURL != "" {
		base = filepath.Join(poolDir, compute.ManagedImagePrefix+imageSHA+".qcow2")
	}
	mustVir("pool-refresh", name)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	password := uuid.NewString()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(root, "vmango.conf")
	write(filepath.Join(root, "keys"), "")
	configuration := fmt.Sprintf(`key_file = %q
cloud_init_templates_dir = %q
namespace_directory = %q
tasks { directory = %q workers = 2 }
api {
 listen = %q
 jwt_signing_key_file = %q
 session_dir = %q
 token_dir = %q
 user "acceptance" { hashed_password = %q }
}
`, filepath.Join(root, "keys"), filepath.Join(root, "cloud-init-templates"), filepath.Join(root, "namespaces"), filepath.Join(root, "tasks"), address, filepath.Join(root, "jwt-key"), filepath.Join(root, "sessions"), filepath.Join(root, "tokens"), string(hash))

	guestSystem := os.Getenv("ACCEPTANCE_GUEST_SYSTEM") == "1"
	clockMode := os.Getenv("ACCEPTANCE_CLOCK_MODE")
	if clockMode == "" {
		clockMode = "timezone"
	}
	if clockMode != "utc" && clockMode != "localtime" && clockMode != "timezone" {
		t.Fatal("ACCEPTANCE_CLOCK_MODE must be utc, localtime or timezone")
	}
	guestPolicy := ""
	if guestSystem {
		guestPolicy = fmt.Sprintf(`clock {mode=%q`, clockMode)
		if clockMode == "timezone" {
			guestPolicy += ` timezone="Europe/Berlin"`
		}
		guestPolicy += "}"
		if arch == "x86_64" {
			guestPolicy += ` smbios { bios {vendor="Vmango" version="1.0" date="01/02/2026" release="1.2"} system {manufacturer="Vmango" product="Acceptance Guest" version="1" sku="test" family="Cloud"} baseboard {manufacturer="Vmango" product="Acceptance Board" version="2"} } timer "rtc" {tickpolicy="catchup"} timer "pit" {tickpolicy="catchup"} timer "hpet" {present=false} timer "kvmclock" {present=true} timer "hypervclock" {present=true}`
		}
	}
	machineAcceleration := os.Getenv("ACCEPTANCE_VIRT_TYPE")
	if machineAcceleration == "" {
		machineAcceleration = "hardware"
	}
	machineType := os.Getenv("ACCEPTANCE_MACHINE_TYPE")
	if machineType == "" {
		machineType = "auto"
	}
	configuration += fmt.Sprintf(`
cloud "acceptance" {
 hardware_profile "boot" { %s machine { type = %q acceleration = %q } firmware = %q disk_bus = "scsi" disk_controller = "virtio-scsi" guest_agent = false graphics = "vnc" video_model = "none" config_drive { format = %q device_type = %q bus = %q } }
 hardware_profile "no-init" { %s machine { type = %q acceleration = %q } firmware = %q config_drive { enabled = false } }
 image "boot" { path = %q os_name = "Ubuntu" os_arch = %q hardware_profile = "boot" protected = true minimum_memory_mib = 1024 minimum_disk_gib = 8 }
 image "no-init" { path = %q os_name = "Ubuntu" os_arch = %q hardware_profile = "no-init" }
 zone "test" {}
 instance_type "small" { cpu=2 memory_mib=2048 boot_disk_gib=8 }
 instance_type "too-small" { cpu=2 memory_mib=512 boot_disk_gib=8 }
 instance_type "larger" { cpu=2 memory_mib=3072 boot_disk_gib=16 }
 network "lan" {}
 storage_class "standard" {}
 storage_class "efficient" { name="Space-efficient" description="Shares its source." clone_mode="overlay" }
 storage_class "raw" { clone_mode="copy" }
 host "acceptance" {
 uri = %q
 config_drive_pool = %q
 image_pool = %q
 qcow_prealloc_metadata = false
 zone="test" labels={rack="test"} network "lan" { network="qemu-usernet" } storage_class "standard" {pool=%q format="qcow2"}
 storage_class "efficient" {pool=%q format="qcow2"}
 storage_class "raw" {pool=%q format="raw"} }
}
`, guestPolicy, machineType, machineAcceleration, firmware, initialization, configDriveDevice, configDriveBus, guestPolicy, machineType, machineAcceleration, firmware, base, arch, base, arch, uri, name, name, name, name, name)
	if imageURL != "" {
		if strings.Contains(imageURL, "alpinelinux.org/") {
			configuration = strings.ReplaceAll(configuration, `os_name = "Ubuntu"`, `os_name = "Alpine Linux"`)
		}
		configuration = strings.ReplaceAll(configuration, fmt.Sprintf("path = %q", base), fmt.Sprintf("url = %q sha256 = %q source_format = \"qcow2\"", imageURL, imageSHA))
		if sourceBytes > 0 {
			configuration = strings.ReplaceAll(configuration, `source_format = "qcow2"`, fmt.Sprintf(`source_format = "qcow2" source_size_bytes = %d`, sourceBytes))
		}
	}
	// This separate unreachable backend exercises configured-vs-live behavior
	// without sharing or inspecting another physical host.
	configuration += fmt.Sprintf(`
cloud "offline" {
 hardware_profile "offline" { firmware=%q config_drive { enabled=false } }
 image "boot" { name="Offline definition" path=%q os_name="Independent OS" os_arch=%q hardware_profile="offline" }
 zone "offline-zone" {}
 instance_type "small" { cpu=2 memory_mib=2048 boot_disk_gib=8 }
 network "lan" {}
 storage_class "standard" {}
 host "offline" { uri=%q zone="offline-zone" network "lan" { network="qemu-usernet" } storage_class "standard" {pool="offline" format="qcow2"} }
}
`, firmware, filepath.Join(root, "offline-source.qcow2"), arch, "qemu+unix:///session?socket="+filepath.Join(root, "offline.sock"))
	platformNamespace := `namespace "platform" { name="Platform" clouds=["acceptance", "offline"] }` + "\n"
	configuration += "\n" + platformNamespace + `namespace "analytics" { name="Analytics" clouds=["acceptance"] }` + "\n"
	write(config, configuration)
	log, err := os.Create(filepath.Join(root, "vmango.log"))
	if err != nil {
		t.Fatal(err)
	}
	binary, err := filepath.Abs("../bin/vmango")
	if err != nil {
		t.Fatal(err)
	}
	app := exec.Command(binary, "web", "-c", config)
	app.Stdout = log
	app.Stderr = log
	if err := app.Start(); err != nil {
		log.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { app.Process.Kill(); app.Wait(); log.Close(); os.Remove(config) })
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 2 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	origin := "http://" + address
	ready := false
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); {
		resp, err := client.Get(origin + "/login/")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				ready = true
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		t.Fatal("app didn't become ready; see vmango.log")
	}
	request := func(path string, status int) {
		t.Helper()
		resp, err := client.Get(origin + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != status {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("%s: %d want %d: %s", path, resp.StatusCode, status, body)
		}
	}
	access := ""
	selectedNamespace := "platform"
	apiRequest := func(method, path string, value any, status int) []byte {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest(method, origin+path, strings.NewReader(string(data)))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		for _, prefix := range []string{"/api/v1/clouds", "/api/v1/keys", "/api/v1/cloud-init-templates", "/api/v1/tasks", "/api/v1/namespaces/"} {
			if strings.HasPrefix(path, prefix) && selectedNamespace != "" {
				req.Header.Set("X-Vmango-Namespace", selectedNamespace)
				break
			}
		}
		if method != "GET" && strings.Contains(path, "/clouds/") && !strings.Contains(path, "/consoles/") {
			req.Header.Set("Idempotency-Key", uuid.NewString())
		}
		if access != "" {
			req.Header.Set("Authorization", "Bearer "+access)
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		if status != 0 && response.StatusCode != status {
			t.Fatalf("%s %s: %d, want %d: %s", method, path, response.StatusCode, status, body)
		}
		if status == 0 && response.StatusCode != 202 && response.StatusCode != 409 && response.StatusCode != 422 {
			t.Fatalf("unexpected rejection response %d: %s", response.StatusCode, body)
		}
		return body
	}
	// Infrastructure commands must be durably accepted before observation.
	apiCommand := func(method, path string, value any) []byte {
		t.Helper()
		raw := apiRequest(method, path, value, 202)
		var accepted struct {
			Task      tasks.Task `json:"task"`
			StatusURL string     `json:"status_url"`
		}
		if err := json.Unmarshal(raw, &accepted); err != nil || accepted.StatusURL == "" {
			t.Fatalf("invalid task response: %s", raw)
		}
		deadline := time.Now().Add(8 * time.Minute)
		for time.Now().Before(deadline) {
			var task tasks.Task
			if err := json.Unmarshal(apiRequest("GET", accepted.StatusURL, nil, 200), &task); err != nil {
				t.Fatal(err)
			}
			// Capture exact backend identities before reporting any partial failure.
			if task.Kind == "machine.create" {
				for _, ref := range task.Resources {
					if ref.Kind == "machine" {
						ownedDomains[ref.ID] = true
						lastCreatedVM = ref.ID
					}
				}
			}
			if task.State.Terminal() {
				if task.State != tasks.Succeeded {
					t.Fatalf("infrastructure task %s: %+v", task.ID, task)
				}
				if imageURL != "" && task.Kind == "machine.create" {
					data, _ := json.MarshalIndent(task, "", "  ")
					write(filepath.Join(root, "create-task-"+task.ID+".json"), string(data))
				}
				if strings.HasPrefix(task.Kind, "volume.") && (task.Kind == "volume.create" || task.Kind == "volume.clone") {
					for _, effect := range task.Effects {
						if effect.Action == "created" && effect.Resource.Kind == "volume" {
							return apiRequest("GET", "/api/v1/clouds/acceptance/volumes/"+(compute.VolumeAddress{Host: effect.Resource.Scope, Path: effect.Resource.ID}).ID(), nil, 200)
						}
					}
					t.Fatal("created volume missing from task effects")
				}
				return raw
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatal("infrastructure task did not finish")
		return nil
	}
	var credentials struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(apiRequest("POST", "/api/v1/auth/login", map[string]string{"username": "acceptance", "password": password}, 200), &credentials); err != nil {
		t.Fatal(err)
	}
	access = credentials.AccessToken
	selectedNamespace = ""
	apiRequest("GET", "/api/v1/clouds", nil, 400)
	apiRequest("GET", "/api/v1/administration/clouds", nil, 200)
	var discovered struct {
		Items []compute.Namespace `json:"items"`
	}
	if err := json.Unmarshal(apiRequest("GET", "/api/v1/namespaces", nil, 200), &discovered); err != nil || len(discovered.Items) != 2 {
		t.Fatal("namespace bootstrap discovery failed", err)
	}
	selectedNamespace = "platform, analytics"
	apiRequest("GET", "/api/v1/clouds", nil, 400)
	selectedNamespace = "missing"
	apiRequest("GET", "/api/v1/clouds", nil, 404)
	selectedNamespace = "analytics"
	apiRequest("GET", "/api/v1/clouds/offline", nil, 404)
	selectedNamespace = "platform"
	apiRequest("GET", "/api/v1/clouds?namespace=platform", nil, 400)
	for _, resource := range []string{"administration/configuration", "administration/clouds", "administration/clouds/acceptance", "administration/clouds/acceptance/configuration", "administration/clouds/acceptance/configuration/hosts", "administration/clouds/acceptance/configuration/hosts/acceptance", "administration/clouds/acceptance/images", "administration/clouds/acceptance/images/boot", "administration/clouds/acceptance/images/boot/availability", "clouds", "clouds/acceptance", "clouds/acceptance/machines", "administration/clouds/acceptance/hosts", "administration/clouds/acceptance/hosts/acceptance", "administration/clouds/acceptance/hosts/acceptance/pools", "administration/clouds/acceptance/hosts/acceptance/networks", "administration/clouds/acceptance/hosts/acceptance/capabilities", "administration/clouds/acceptance/hosts/acceptance/catalog", "keys", "clouds/acceptance/volumes"} {
		apiRequest("GET", "/api/v1/"+resource, nil, 200)
	}
	for _, removed := range []string{"hosts", "hosts/acceptance", "hosts/acceptance/networks", "hosts/acceptance/pools", "hosts/acceptance/capabilities", "hosts/acceptance/catalog", "hosts/acceptance/images", "hosts/acceptance/cloud"} {
		apiRequest("GET", "/api/v1/"+removed, nil, 404)
	}
	request("/nodes/", 404)
	request("/nodes/acceptance/", 404)
	request("/administration/clouds/acceptance/images/boot/", 200)
	var originalSnapshot compute.AdministrationInstallation
	if err := json.Unmarshal(apiRequest("GET", "/api/v1/administration/configuration", nil, 200), &originalSnapshot); err != nil {
		t.Fatal(err)
	}
	write(config, "log_level=\"debug\"\n"+configuration)
	var unchangedSnapshot compute.AdministrationInstallation
	if err := json.Unmarshal(apiRequest("GET", "/api/v1/administration/configuration", nil, 200), &unchangedSnapshot); err != nil {
		t.Fatal(err)
	}
	if originalSnapshot != unchangedSnapshot {
		t.Fatal("editing configuration changed the accepted running snapshot")
	}
	write(config, configuration)
	var offlineImage compute.AdministrationImage
	if err := json.Unmarshal(apiRequest("GET", "/api/v1/administration/clouds/offline/images/boot", nil, 200), &offlineImage); err != nil {
		t.Fatal(err)
	}
	if offlineImage.Name != "Offline definition" || offlineImage.CloudID != "offline" || offlineImage.OSName != "Independent OS" {
		t.Fatal("same-ID image definition crossed cloud scope", offlineImage)
	}
	apiRequest("GET", "/api/v1/administration/clouds/offline/configuration/hosts/offline", nil, 200)
	apiRequest("GET", "/api/v1/administration/clouds/acceptance/hosts/offline", nil, 404)
	apiRequest("GET", "/api/v1/administration/clouds/offline/hosts/offline", nil, 503)
	var offlineHosts struct {
		Items       []json.RawMessage                       `json:"items"`
		Unavailable []compute.AdministrationUnavailableHost `json:"unavailable_hosts"`
	}
	if err := json.Unmarshal(apiRequest("GET", "/api/v1/administration/clouds/offline/hosts", nil, 200), &offlineHosts); err != nil {
		t.Fatal(err)
	}
	if len(offlineHosts.Items) != 0 || len(offlineHosts.Unavailable) != 1 || offlineHosts.Unavailable[0].ID != "offline" {
		t.Fatal("offline host inventory was treated as confirmed empty", offlineHosts)
	}
	var offlineVolumes struct {
		Items       []json.RawMessage `json:"items"`
		Unavailable []string          `json:"unavailable_hosts"`
	}
	if err := json.Unmarshal(apiRequest("GET", "/api/v1/clouds/offline/volumes", nil, 200), &offlineVolumes); err != nil {
		t.Fatal(err)
	}
	if len(offlineVolumes.Items) != 0 || len(offlineVolumes.Unavailable) != 1 || offlineVolumes.Unavailable[0] != "offline" {
		t.Fatal("offline volume inventory was treated as confirmed empty", offlineVolumes)
	}
	t.Log("Administration retained accepted configuration with an offline cloud, independent same-ID image definitions and explicit unavailable inventory")
	if imageURL != "" {
		if _, err := vir("vol-info", base); err == nil {
			t.Fatal("catalog listing downloaded an image")
		}
		var availability compute.AdministrationImageAvailability
		if err := json.Unmarshal(apiRequest("GET", "/api/v1/administration/clouds/acceptance/images/boot/availability", nil, 200), &availability); err != nil {
			t.Fatal(err)
		}
		if availability.Libvirt == nil || len(availability.Libvirt.Hosts) != 1 || availability.Libvirt.Hosts[0].SourceState != "downloadable" {
			t.Fatalf("missing managed image unavailable: %+v", availability)
		}
	}
	request("/machines/add/?cloud=acceptance", 200)
	marker := "VMANGO_BOOT_OK_" + uuid.NewString()
	serialDevice := "/dev/ttyS0"
	if arch == "aarch64" {
		serialDevice = "/dev/ttyAMA0"
	}
	userdata := fmt.Sprintf("#cloud-config\nruncmd:\n  - [sh, -c, 'test \"$(hostname)\" = %s && echo %s > %s']\n", name, marker, serialDevice)
	if guestSystem {
		commands := ""
		if arch == "x86_64" {
			for _, field := range [][2]string{{"bios_vendor", "Vmango"}, {"bios_version", "1.0"}, {"bios_date", "01/02/2026"}, {"sys_vendor", "Vmango"}, {"product_name", "Acceptance Guest"}, {"product_version", "1"}, {"product_sku", "test"}, {"product_family", "Cloud"}, {"board_vendor", "Vmango"}, {"board_name", "Acceptance Board"}, {"board_version", "2"}} {
				commands += fmt.Sprintf(`test "$(cat /sys/class/dmi/id/%s)" = "%s" && `, field[0], field[1])
			}
			commands += fmt.Sprintf(`echo VMANGO_DMI_UUID=$(cat /sys/class/dmi/id/product_uuid) > %s && `, serialDevice)
		}
		commands += fmt.Sprintf(`echo VMANGO_RTC=$(cat /sys/class/rtc/rtc0/date)T$(cat /sys/class/rtc/rtc0/time) > %s && echo VMANGO_CLOCKSOURCE=$(cat /sys/devices/system/clocksource/clocksource0/current_clocksource) > %s && `, serialDevice, serialDevice)
		userdata = fmt.Sprintf("#cloud-config\nruncmd:\n  - [sh, -c, '%stest \"$(hostname)\" = %s && echo %s > %s']\n", commands, name, marker, serialDevice)
	}
	// Check image constraints and the no-init alias through the same JSON API.
	if os.Getenv("ACCEPTANCE_STORAGE_ONLY") == "1" {
		testStorageCloning(t, root, name, "", apiRequest, apiCommand, mustVir, run)
		return
	}
	aliasBody := map[string]any{"name": name, "image": "boot", "instance_type": "too-small", "zone": "test", "network": "lan", "storage_class": "standard", "start": false}
	apiRequest("POST", "/api/v1/clouds/acceptance/machines", aliasBody, 422)
	if _, err := vir("vol-info", "--pool", name, name+"_root"); err == nil {
		t.Fatal("invalid request created a disk")
	}
	aliasBody["instance_type"] = "small"
	aliasBody["image"] = "no-init"

	if os.Getenv("ACCEPTANCE_CAPACITY_REJECTION") == "1" {
		raw := apiRequest("POST", "/api/v1/clouds/acceptance/machines", aliasBody, 202)
		var accepted struct {
			StatusURL string `json:"status_url"`
		}
		json.Unmarshal(raw, &accepted)
		for deadline := time.Now().Add(30 * time.Second); ; {
			var task tasks.Task
			json.Unmarshal(apiRequest("GET", accepted.StatusURL, nil, 200), &task)
			for _, ref := range task.Resources {
				if ref.Kind == "machine" {
					ownedDomains[ref.ID] = true
				}
			}
			if task.State.Terminal() {
				if task.State != tasks.Failed || task.OutcomeUncertain || len(task.Effects) != 0 || task.Error == nil || !strings.Contains(task.Error.Message, "capacity") {
					t.Fatalf("unexpected admission result: %+v", task)
				}
				data, _ := json.MarshalIndent(task, "", "  ")
				write(filepath.Join(root, "capacity-rejection.json"), string(data))
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("capacity rejection did not finish")
			}
			time.Sleep(50 * time.Millisecond)
		}
		if _, err := vir("dominfo", name); err == nil {
			t.Fatal("rejected placement created a VM")
		}
		if _, err := vir("vol-info", "--pool", name, name+"_root"); err == nil {
			t.Fatal("rejected placement created a root disk")
		}
		t.Log("PASS: overcommitted real host rejected placement before provisioning; lifecycle was not run")
		return
	}
	aliasBody["start"] = true
	apiCommand("POST", "/api/v1/clouds/acceptance/machines", aliasBody)
	if state := strings.TrimSpace(mustVir("domstate", lastCreatedVM)); state != "running" {
		t.Fatalf("create-and-start did not start its VM: %s", state)
	}
	if !strings.Contains(mustVir("dominfo", lastCreatedVM), "enable") {
		t.Fatal("successful create-and-start did not enable autostart")
	}
	apiCommand("POST", "/api/v1/clouds/acceptance/machines/"+lastCreatedVM+"/actions", map[string]string{"action": "poweroff"})
	if !strings.Contains(mustVir("dominfo", lastCreatedVM), "disable") {
		t.Fatal("successful poweroff did not disable autostart")
	}
	aliasBody["start"] = false
	if imageURL != "" {
		if _, err := vir("vol-info", base); err != nil {
			t.Fatal("verified image missing", err)
		}
		var readyAvailability compute.AdministrationImageAvailability
		if err := json.Unmarshal(apiRequest("GET", "/api/v1/administration/clouds/acceptance/images/boot/availability", nil, 200), &readyAvailability); err != nil {
			t.Fatal(err)
		}
		if readyAvailability.Libvirt == nil || len(readyAvailability.Libvirt.Hosts) != 1 || readyAvailability.Libvirt.Hosts[0].SourceState != "ready" {
			t.Fatal("verified reusable cache is not reported ready", readyAvailability)
		}
	}
	domainUUID = lastCreatedVM
	noInitXML := mustVir("dumpxml", domainUUID)
	if strings.Contains(noInitXML, "_config.iso") || !strings.Contains(noInitXML, "org.qemu.guest_agent.0") {
		t.Fatal("no-init alias defaults were not applied")
	}

	apiCommand("POST", "/api/v1/clouds/acceptance/machines/"+domainUUID+"/instance-type", map[string]any{"instance_type": "larger"})
	changedXML := mustVir("dumpxml", domainUUID)
	var beforePlatform, afterPlatform libvirtxml.Domain
	if err := beforePlatform.Unmarshal(noInitXML); err != nil {
		t.Fatal(err)
	}
	if err := afterPlatform.Unmarshal(changedXML); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(beforePlatform.SysInfo, afterPlatform.SysInfo) || !reflect.DeepEqual(beforePlatform.Clock, afterPlatform.Clock) {
		t.Fatal("resource change replaced guest system settings")
	}
	if beforePlatform.Type != afterPlatform.Type || beforePlatform.OS.Type.Machine != afterPlatform.OS.Type.Machine || beforePlatform.Devices.Emulator != afterPlatform.Devices.Emulator {
		t.Fatal("resource change replaced platform binding")
	}

	if !strings.Contains(mustVir("dominfo", domainUUID), "disable") || !strings.Contains(mustVir("domstate", domainUUID), "shut off") || !strings.Contains(changedXML, "org.qemu.guest_agent.0") || !strings.Contains(changedXML, "127.0.0.1") {
		t.Fatal("type change did not preserve disabled autostart, power, devices and listener")
	}
	if !strings.Contains(changedXML, "3145728") {
		t.Fatal("instance type RAM change missing from domain XML")
	}
	var noInitDomain libvirtxml.Domain
	if err := noInitDomain.Unmarshal(noInitXML); err != nil {
		t.Fatal(err)
	}
	if noInitDomain.Devices == nil || len(noInitDomain.Devices.Disks) == 0 || noInitDomain.Devices.Disks[0].Source == nil || noInitDomain.Devices.Disks[0].Source.File == nil {
		t.Fatal("root disk source missing")
	}
	rootDiskPath := noInitDomain.Devices.Disks[0].Source.File.File
	info := mustVir("vol-info", rootDiskPath, "--bytes")
	if !strings.Contains(info, "8589934592") {
		t.Fatal("instance type change resized the boot disk", info)
	}
	// Simulate an unmanaged domain without changing any resources or claiming disks.
	mustVir("metadata", domainUUID, "--config", "--uri", "https://vmango.org/xmlns/scheduler/1", "--remove")
	var observed struct {
		Managed bool `json:"managed"`
	}
	json.Unmarshal(apiRequest("GET", "/api/v1/clouds/acceptance/machines/"+domainUUID, nil, 200), &observed)
	if observed.Managed {
		t.Fatal("unmanaged machine was inferred from its name")
	}
	apiRequest("POST", "/api/v1/clouds/acceptance/machines/"+domainUUID+"/actions", map[string]string{"action": "start"}, 409)
	apiCommand("POST", "/api/v1/clouds/acceptance/machines/"+domainUUID+"/adopt", map[string]any{"hardware_profile": "no-init"})
	if !strings.Contains(mustVir("dumpxml", domainUUID), "3145728") {
		t.Fatal("adoption changed resource configuration")
	}
	apiCommand("DELETE", "/api/v1/clouds/acceptance/machines/"+domainUUID+"?delete_volumes=true", nil)
	domainUUID = ""
	apiVMPath := ""
	var userDataTemplate compute.CloudInitTemplate
	if err := json.Unmarshal(apiRequest("POST", "/api/v1/cloud-init-templates", map[string]string{"name": "Acceptance provisioning", "user_data": userdata}, 201), &userDataTemplate); err != nil {
		t.Fatal(err)
	}
	if userDataTemplate.ID == "" || userDataTemplate.Revision == "" || userDataTemplate.UserData != userdata {
		t.Fatal("template content or identity changed")
	}
	templatePath := "/api/v1/cloud-init-templates/" + userDataTemplate.ID
	baseDigest := func(label string) [32]byte {
		t.Helper()
		target := filepath.Join(root, label)
		mustVir("vol-download", "--pool", name, base, target)
		data, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		return sha256.Sum256(data)
	}
	var initialBaseDigest [32]byte
	bootClass := "standard"
	if os.Getenv("ACCEPTANCE_OVERLAY") == "1" {
		bootClass = "efficient"
		initialBaseDigest = baseDigest("boot-base-before.qcow2")
	}
	createBody := map[string]any{"name": name, "image": "boot", "instance_type": "small", "zone": "test", "network": "lan", "storage_class": bootClass, "start": false, "user_data_template": compute.CloudInitTemplateRef{ID: userDataTemplate.ID, Revision: userDataTemplate.Revision}}
	apiCommand("POST", "/api/v1/clouds/acceptance/machines", createBody)
	domainUUID = lastCreatedVM
	apiVMPath = "/api/v1/clouds/acceptance/machines/" + domainUUID
	primaryUUID := domainUUID
	primaryBackendName := strings.TrimSpace(mustVir("domname", primaryUUID))
	selectedNamespace = "analytics"
	apiRequest("GET", apiVMPath, nil, 404)
	apiRequest("POST", apiVMPath+"/actions", map[string]string{"action": "start"}, 404)
	apiRequest("GET", templatePath, nil, 404)
	// A second namespace may use the same label on the same host. Its backend
	// identity and disks must remain independent of the first machine.
	otherRaw := apiCommand("POST", "/api/v1/clouds/acceptance/machines", aliasBody)
	otherUUID := lastCreatedVM
	if otherUUID == primaryUUID || strings.TrimSpace(mustVir("domname", otherUUID)) == primaryBackendName {
		t.Fatal("equal labels reused a physical machine identity")
	}
	var otherAccepted struct {
		Task tasks.Task `json:"task"`
	}
	if err := json.Unmarshal(otherRaw, &otherAccepted); err != nil {
		t.Fatal(err)
	}
	apiCommand("DELETE", "/api/v1/clouds/acceptance/machines/"+otherUUID+"?delete_volumes=true", nil)
	selectedNamespace = "platform"
	apiRequest("GET", "/api/v1/tasks/"+otherAccepted.Task.ID, nil, 404)
	apiRequest("GET", "/api/v1/administration/tasks/"+otherAccepted.Task.ID, nil, 200)
	lastCreatedVM = primaryUUID
	domainUUID = primaryUUID
	// Logical labels are reserved within namespace + cloud before acceptance.
	apiRequest("POST", "/api/v1/clouds/acceptance/machines", createBody, 409)
	// Removing a reusable template must not change the already provisioned guest.
	apiRequest("DELETE", templatePath, map[string]string{"revision": userDataTemplate.Revision}, 204)
	apiRequest("GET", templatePath, nil, 404)
	apiRequest("PATCH", apiVMPath, map[string]any{"autostart": true}, 405)
	domainUUID = lastCreatedVM
	write(filepath.Join(root, "resources.txt"), "pool="+name+"\npool_uuid="+poolUUID+"\npool_path="+poolDir+"\nvm_uuid="+domainUUID+"\n")
	path := "/machines/acceptance/" + domainUUID + "/"
	xml := mustVir("dumpxml", domainUUID)
	write(filepath.Join(root, "domain.xml"), xml)
	var runtimeDomain libvirtxml.Domain
	if err := runtimeDomain.Unmarshal(xml); err != nil {
		t.Fatal(err)
	}
	if guestSystem {
		if runtimeDomain.Clock == nil || runtimeDomain.Clock.Offset != clockMode || clockMode == "timezone" && runtimeDomain.Clock.TimeZone != "Europe/Berlin" {
			t.Fatal("clock policy missing from XML")
		}
	}
	if bootClass == "efficient" {
		if runtimeDomain.Devices == nil || len(runtimeDomain.Devices.Disks) == 0 || runtimeDomain.Devices.Disks[0].Source == nil || runtimeDomain.Devices.Disks[0].Source.File == nil {
			t.Fatal("missing overlay boot disk")
		}
		var bootVolume libvirtxml.StorageVolume
		if err := bootVolume.Unmarshal(mustVir("vol-dumpxml", "--pool", name, runtimeDomain.Devices.Disks[0].Source.File.File)); err != nil {
			t.Fatal(err)
		}
		if bootVolume.BackingStore == nil || bootVolume.BackingStore.Path != base {
			t.Fatal("boot disk does not depend on selected image")
		}
	}
	if runtimeDomain.OS.Type.Machine == "" || runtimeDomain.OS.Type.Machine == "auto" || runtimeDomain.Devices.Emulator == "" {
		t.Fatal("missing concrete platform binding")
	}
	if runtimeDomain.Type != expectedType {
		t.Fatalf("expected virtualization type %s, got %s; see domain.xml", expectedType, runtimeDomain.Type)
	}
	if !strings.Contains(xml, "virtio-scsi") || strings.Contains(xml, "org.qemu.guest_agent.0") || !strings.Contains(xml, "_config.iso") {
		t.Fatal("image launch defaults did not reach domain XML")
	}
	foundConfigDrive := false
	for _, disk := range runtimeDomain.Devices.Disks {
		if disk.Alias != nil && disk.Alias.Name == "ua-configdrive" {
			foundConfigDrive = true
			if disk.Device != configDriveDevice || disk.Target == nil || disk.Target.Bus != configDriveBus || disk.Boot != nil {
				t.Fatalf("config-drive attachment/boot policy mismatch: %+v", disk)
			}
		}
	}
	if !foundConfigDrive {
		t.Fatal("config-drive attachment missing")
	}
	if adoptPower {
		// Adoption records explicit compatible policy without rewriting hardware.
		mustVir("metadata", domainUUID, "--config", "--uri", "https://vmango.org/xmlns/scheduler/1", "--remove")
		apiCommand("POST", apiVMPath+"/adopt", map[string]any{"hardware_profile": "boot"})
		adoptedXML := mustVir("dumpxml", domainUUID, "--inactive")
		write(filepath.Join(root, "adopted-domain.xml"), adoptedXML)
		var before, after libvirtxml.Domain
		if err := before.Unmarshal(xml); err != nil {
			t.Fatal(err)
		}
		if err := after.Unmarshal(adoptedXML); err != nil {
			t.Fatal(err)
		}
		before.Metadata, after.Metadata = nil, nil
		beforeXML, _ := before.Marshal()
		afterXML, _ := after.Marshal()
		if beforeXML != afterXML {
			t.Fatal("adoption changed existing domain hardware or power configuration")
		}
		xml = adoptedXML
	}
	request(path, 200)
	t.Log("created VM and cloud-init disk through HTTP; starting guest")
	apiCommand("POST", apiVMPath+"/actions", map[string]string{"action": "start"})
	if state := strings.TrimSpace(mustVir("domstate", domainUUID)); state != "running" {
		t.Fatalf("expected running, got %s", state)
	}
	if !strings.Contains(mustVir("dominfo", domainUUID), "enable") {
		t.Fatal("successful start did not enable autostart")
	}
	headers := http.Header{"Origin": {origin}, "Authorization": {"Bearer " + access}, "X-Vmango-Namespace": {selectedNamespace}}
	u, _ := url.Parse(origin)
	for _, c := range jar.Cookies(u) {
		headers.Add("Cookie", c.String())
	}
	dial := func(suffix string) *websocket.Conn {
		t.Helper()
		conn, resp, err := websocket.DefaultDialer.Dial("ws://"+address+apiVMPath+"/consoles/"+suffix, headers)
		if err != nil {
			if resp != nil {
				resp.Body.Close()
			}
			t.Fatal(err)
		}
		return conn
	}
	console := dial("serial/ws")
	defer console.Close()
	console.SetReadDeadline(time.Now().Add(bootTimeout))
	consoleLog, err := os.Create(filepath.Join(root, "console.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer consoleLog.Close()
	vnc := dial("vnc/ws")
	vnc.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, greeting, err := vnc.ReadMessage()
	vnc.Close()
	if err != nil || !strings.HasPrefix(string(greeting), "RFB ") {
		t.Fatalf("VNC handshake: %q %v", greeting, err)
	}
	t.Log("VNC proxy returned RFB greeting; waiting for cloud-init boot marker")
	tail := ""
	for !strings.Contains(tail, marker) {
		_, data, err := console.ReadMessage()
		if err != nil {
			t.Fatalf("guest boot marker missing: %v; see console.log", err)
		}
		consoleLog.Write(data)
		tail += string(data)
		if len(tail) > 65536 {
			tail = tail[len(tail)-65536:]
		}
	}
	console.Close()
	if guestSystem {
		if arch == "x86_64" && !strings.Contains(strings.ToLower(tail), "vmango_dmi_uuid="+strings.ToLower(domainUUID)) {
			t.Fatal("guest SMBIOS UUID differs from domain UUID; see console.log")
		}
		if !strings.Contains(tail, "VMANGO_RTC=") || !strings.Contains(tail, "VMANGO_CLOCKSOURCE=") {
			t.Fatal("guest clock observation missing")
		}
		t.Log("guest-visible SMBIOS values/UUID checked where configured; RTC and clocksource recorded in console.log")
	}
	t.Log("guest ran cloud-init userdata and confirmed its hostname")
	// Test signed browser transports without browser refresh cookies or resource Bearer headers.
	for _, kind := range []string{"serial", "vnc"} {
		var link struct {
			URL string `json:"url"`
		}
		raw := apiRequest("POST", apiVMPath+"/consoles/"+kind+"/links", map[string]bool{"force": kind == "serial"}, 201)
		if err := json.Unmarshal(raw, &link); err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(link.URL)
		if err != nil {
			t.Fatal(err)
		}
		d := websocket.Dialer{Subprotocols: []string{"binary", "vmango.token." + u.Fragment}}
		for n := 0; n < 2; n++ {
			c, response, err := d.Dial("ws://"+address+u.Path+"/ws", http.Header{"Origin": {origin}})
			if err != nil {
				if response != nil {
					response.Body.Close()
				}
				t.Fatalf("signed %s reconnect: %v", kind, err)
			}
			if kind == "vnc" {
				c.SetReadDeadline(time.Now().Add(10 * time.Second))
				_, data, err := c.ReadMessage()
				if err != nil || !strings.HasPrefix(string(data), "RFB ") {
					t.Fatalf("signed VNC greeting: %q %v", data, err)
				}
			} else {
				other, response, err := websocket.DefaultDialer.Dial("ws://"+address+apiVMPath+"/consoles/serial/ws", headers)
				if err == nil {
					other.Close()
					t.Fatal("second serial client silently displaced first")
				}
				if response == nil || response.StatusCode != 409 {
					t.Fatalf("second serial client: %v %v", response, err)
				}
				response.Body.Close()
			}
			c.Close()
		}
	}
	t.Log("signed serial/VNC links reconnected; second serial client rejected")
	if adoptPower {
		apiCommand("POST", apiVMPath+"/actions", map[string]string{"action": "shutdown"})
		for deadline := time.Now().Add(90 * time.Second); ; {
			if strings.TrimSpace(mustVir("domstate", domainUUID)) == "shut off" {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("adopted guest did not shut down gracefully")
			}
			time.Sleep(500 * time.Millisecond)
		}
		time.Sleep(2 * time.Second)
		if strings.TrimSpace(mustVir("domstate", domainUUID)) != "shut off" {
			t.Fatal("adopted guest restarted after shutdown")
		}
		if !strings.Contains(mustVir("dominfo", domainUUID), "disable") {
			t.Fatal("confirmed shutdown did not disable autostart")
		}
		t.Log("adopted guest shut down gracefully and stayed powered off")
		apiCommand("POST", apiVMPath+"/actions", map[string]string{"action": "start"})
	}
	apiCommand("POST", apiVMPath+"/actions", map[string]string{"action": "poweroff"})
	if state := strings.TrimSpace(mustVir("domstate", domainUUID)); state != "shut off" {
		t.Fatalf("expected shut off, got %s", state)
	}
	if bootClass == "efficient" && initialBaseDigest != baseDigest("boot-base-after.qcow2") {
		t.Fatal("guest boot changed shared image contents")
	}
	testStorageCloning(t, root, name, apiVMPath, apiRequest, apiCommand, mustVir, run)
	// Exercise the remaining mutating routes only against this isolated VM/pool.
	volumeBase := "/api/v1/clouds/acceptance/volumes"
	var disk struct {
		ID string `json:"id"`
	}
	rawDisk := apiCommand("POST", "/api/v1/clouds/acceptance/volumes", map[string]any{"name": "extra", "zone": "test", "storage_class": "standard", "size_bytes": 16 << 20})
	if err := json.Unmarshal(rawDisk, &disk); err != nil {
		t.Fatal(err)
	}
	selectedNamespace = "analytics"
	apiRequest("GET", volumeBase+"/"+disk.ID, nil, 404)
	apiRequest("POST", volumeBase+"/"+disk.ID+"/resize", map[string]any{"size_bytes": 32 << 20}, 404)
	var otherDisk struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(apiCommand("POST", volumeBase, map[string]any{"name": "extra", "zone": "test", "storage_class": "standard", "size_bytes": 16 << 20}), &otherDisk); err != nil {
		t.Fatal(err)
	}
	if otherDisk.ID == disk.ID {
		t.Fatal("equal volume labels reused the same backend volume")
	}
	selectedNamespace = "platform"
	apiRequest("POST", apiVMPath+"/disks/attach", map[string]any{"volume_id": otherDisk.ID}, 404)
	selectedNamespace = "analytics"
	apiCommand("DELETE", volumeBase+"/"+otherDisk.ID, nil)
	selectedNamespace = "platform"
	addressed, err := compute.ParseVolumeID(disk.ID)
	if err != nil || addressed.Host != "acceptance" || addressed.Path == "" {
		t.Fatal("volume did not expose its canonical compound address", disk.ID, err)
	}
	apiRequest("GET", "/api/v1/clouds/offline/volumes/"+disk.ID, nil, 404)
	apiRequest("GET", volumeBase+"/invalid-path-only-id", nil, 422)
	apiRequest("GET", volumeBase+"/acceptance/"+disk.ID, nil, 404)
	var volumeDetail struct {
		Catalog *struct {
			StorageClasses []compute.CatalogEntry `json:"storage_classes"`
		} `json:"catalog"`
	}
	if err := json.Unmarshal(apiRequest("GET", volumeBase+"/"+disk.ID, nil, 200), &volumeDetail); err != nil {
		t.Fatal(err)
	}
	if volumeDetail.Catalog == nil || len(volumeDetail.Catalog.StorageClasses) != 3 {
		t.Fatal("volume detail lacks applicable logical choices", volumeDetail)
	}
	apiCommand("POST", volumeBase+"/"+disk.ID+"/resize", map[string]any{"size_bytes": 32 << 20})
	var clone struct {
		ID string `json:"id"`
	}
	rawClone := apiCommand("POST", volumeBase+"/"+disk.ID+"/clone", map[string]any{"name": "extra-clone", "storage_class": "standard", "size_bytes": 32 << 20})
	if err := json.Unmarshal(rawClone, &clone); err != nil {
		t.Fatal(err)
	}
	apiRequest("POST", apiVMPath+"/disks/attach", map[string]any{"volume_id": disk.ID, "bus": "virtio"}, 422)
	apiCommand("POST", apiVMPath+"/disks/attach", map[string]any{"volume_id": disk.ID})
	var machineDetail struct {
		Disks []struct {
			ID  string `json:"volume_id"`
			Bus string `json:"bus"`
		} `json:"disks"`
		Catalog *struct {
			Networks       []compute.CatalogEntry `json:"networks"`
			StorageClasses []compute.CatalogEntry `json:"storage_classes"`
		} `json:"catalog"`
	}
	if err := json.Unmarshal(apiRequest("GET", apiVMPath, nil, 200), &machineDetail); err != nil {
		t.Fatal(err)
	}
	if machineDetail.Catalog == nil || len(machineDetail.Catalog.Networks) != 1 || len(machineDetail.Catalog.StorageClasses) != 3 {
		t.Fatal("machine detail lacks applicable logical choices", machineDetail)
	}
	attached := false
	for _, d := range machineDetail.Disks {
		if d.ID == disk.ID {
			attached = true
			if d.Bus != "scsi" {
				t.Fatalf("later disk ignored applied SCSI policy: %s", d.Bus)
			}
		}
	}
	if !attached {
		t.Fatal("machine disk ID differs from volume list/detail address", machineDetail.Disks)
	}
	// Restart only after the preceding task completed. The dedicated task/auth
	// directories must retain real references and history across a process restart.
	historyBefore := apiRequest("GET", "/api/v1/tasks?limit=100", nil, 200)
	stopApp := func() {
		t.Helper()
		if err := app.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		_ = app.Wait()
	}
	startApp := func() {
		t.Helper()
		app = exec.Command(binary, "web", "-c", config)
		app.Stdout, app.Stderr = log, log
		if err := app.Start(); err != nil {
			t.Fatal(err)
		}
		for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); {
			resp, err := client.Get(origin + "/login/")
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode == 200 {
					return
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatal("restarted app did not become ready; see vmango.log")
	}
	stopApp()
	startApp()
	historyAfter := apiRequest("GET", "/api/v1/tasks?limit=100", nil, 200)
	var beforeHistory, afterHistory tasks.Page
	if err := json.Unmarshal(historyBefore, &beforeHistory); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(historyAfter, &afterHistory); err != nil {
		t.Fatal(err)
	}
	beforeJSON, _ := json.Marshal(beforeHistory)
	afterJSON, _ := json.Marshal(afterHistory)
	if string(beforeJSON) != string(afterJSON) {
		t.Fatal("restart changed task history/resource references")
	}
	apiRequest("GET", volumeBase+"/"+disk.ID, nil, 200)
	if guestSystem {
		var afterRestart libvirtxml.Domain
		if err := afterRestart.Unmarshal(mustVir("dumpxml", domainUUID, "--inactive")); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(runtimeDomain.SysInfo, afterRestart.SysInfo) || !reflect.DeepEqual(runtimeDomain.Clock, afterRestart.Clock) {
			t.Fatal("restart changed guest system settings")
		}
	}
	t.Log("process restart retained task history, backend references and compound volume navigation")
	// Removing configuration retains authoritative assignments and immutable
	// history. Administration must remain usable without the removed context.
	stopApp()
	write(config, strings.Replace(configuration, platformNamespace, "", 1))
	startApp()
	apiRequest("GET", apiVMPath, nil, 404)
	apiRequest("GET", "/api/v1/tasks", nil, 404)
	var retained struct {
		Namespace string `json:"namespace_id"`
		ID        string `json:"id"`
	}
	if err := json.Unmarshal(apiRequest("GET", "/api/v1/administration/clouds/acceptance/machines/"+domainUUID, nil, 200), &retained); err != nil || retained.Namespace != "platform" || retained.ID != domainUUID {
		t.Fatal("configuration removal lost VM assignment", err, retained)
	}
	apiRequest("GET", "/api/v1/administration/tasks", nil, 200)
	stopApp()
	write(config, configuration)
	startApp()
	apiRequest("GET", apiVMPath, nil, 200)
	if string(apiRequest("GET", "/api/v1/tasks?limit=100", nil, 200)) != string(historyAfter) {
		t.Fatal("removing/re-adding namespace changed saved history")
	}

	// Simulate legacy data only within this run's private VM and stores. Its
	// existing disks keep their assignments; the additional detached disk has none.
	stopApp()
	mustVir("metadata", domainUUID, "--config", "--uri", "https://vmango.org/xmlns/workload/1", "--remove")
	legacyXML := mustVir("dumpxml", domainUUID, "--inactive")
	var legacyDomain libvirtxml.Domain
	if err := legacyDomain.Unmarshal(legacyXML); err != nil {
		t.Fatal(err)
	}
	mustVir("vol-create-as", name, "legacy-migration.qcow2", "8M", "--format", "qcow2")
	legacyVolume := strings.TrimSpace(mustVir("vol-path", "--pool", name, "legacy-migration.qcow2"))
	legacyPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	legacySSH, err := ssh.NewPublicKey(legacyPublic)
	if err != nil {
		t.Fatal(err)
	}
	legacyFingerprint := ssh.FingerprintLegacyMD5(legacySSH)
	write(filepath.Join(root, "keys"), string(ssh.MarshalAuthorizedKey(legacySSH)))
	legacyTemplate := compute.CloudInitTemplate{ID: uuid.NewString(), Revision: uuid.NewString(), Name: "Legacy setup", UserData: "#cloud-config\npackages: [curl]\n"}
	if err := os.MkdirAll(filepath.Join(root, "cloud-init-templates"), 0700); err != nil {
		t.Fatal(err)
	}
	legacyTemplateJSON, _ := json.Marshal(legacyTemplate)
	write(filepath.Join(root, "cloud-init-templates", legacyTemplate.ID+".json"), string(legacyTemplateJSON))
	mappingJSON, _ := json.Marshal(map[string]any{
		"version":   1,
		"resources": []any{map[string]any{"namespace": "platform", "cloud": "acceptance", "machines": []string{domainUUID}, "volumes": []compute.LocalDiskRef{{Host: "acceptance", Path: legacyVolume}}}},
		"catalogs":  []any{map[string]any{"namespace": "platform", "keys": []string{legacyFingerprint}, "templates": []compute.CloudInitTemplateRef{{ID: legacyTemplate.ID, Revision: legacyTemplate.Revision}}}},
	})
	mappingPath, backupPath := filepath.Join(root, "migration.json"), filepath.Join(root, "migration-backup")
	write(mappingPath, string(mappingJSON))
	migrateArgs := []string{"namespace-migrate", "-c", config, "--mapping", mappingPath}
	if out, err := run(binary, migrateArgs...); err != nil {
		t.Fatalf("migration dry-run: %s %v", out, err)
	}
	if after := mustVir("dumpxml", domainUUID, "--inactive"); after != legacyXML {
		t.Fatal("dry-run changed VM XML")
	}
	for attempt := 0; attempt < 2; attempt++ {
		if out, err := run(binary, append(migrateArgs, "--apply", "--backup", backupPath)...); err != nil {
			t.Fatalf("migration apply/resume: %s %v", out, err)
		}
	}
	if _, err := os.Stat(filepath.Join(backupPath, "manifest.json")); err != nil {
		t.Fatal("migration backup manifest missing", err)
	}
	var migratedDomain libvirtxml.Domain
	if err := migratedDomain.Unmarshal(mustVir("dumpxml", domainUUID, "--inactive")); err != nil {
		t.Fatal(err)
	}
	if migratedDomain.UUID != legacyDomain.UUID || migratedDomain.Name != legacyDomain.Name || len(migratedDomain.Devices.Disks) != len(legacyDomain.Devices.Disks) {
		t.Fatal("migration changed resource identity or disks")
	}
	startApp()
	var migrated struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Namespace string `json:"namespace_id"`
	}
	if err := json.Unmarshal(apiRequest("GET", apiVMPath, nil, 200), &migrated); err != nil || migrated.ID != domainUUID || migrated.Namespace != "platform" || migrated.Name != legacyDomain.Name {
		t.Fatal("migration did not preserve legacy label/identity", err, migrated)
	}
	legacyVolumeID := (compute.VolumeAddress{Host: "acceptance", Path: legacyVolume}).ID()
	apiRequest("GET", volumeBase+"/"+legacyVolumeID, nil, 200)
	apiRequest("GET", "/api/v1/keys/"+legacyFingerprint, nil, 200)
	var migratedTemplate compute.CloudInitTemplate
	if err := json.Unmarshal(apiRequest("GET", "/api/v1/cloud-init-templates/"+legacyTemplate.ID, nil, 200), &migratedTemplate); err != nil || migratedTemplate.ID != legacyTemplate.ID || migratedTemplate.Revision != legacyTemplate.Revision || migratedTemplate.UserData != legacyTemplate.UserData {
		t.Fatal("migration changed template identity/revision/content", err)
	}
	selectedNamespace = "analytics"
	apiRequest("GET", "/api/v1/keys/"+legacyFingerprint, nil, 404)
	apiRequest("GET", "/api/v1/cloud-init-templates/"+legacyTemplate.ID, nil, 404)
	apiRequest("GET", volumeBase+"/"+legacyVolumeID, nil, 404)
	selectedNamespace = "platform"
	if string(apiRequest("GET", "/api/v1/tasks?limit=100", nil, 200)) != string(historyAfter) {
		t.Fatal("migration changed historical task attribution")
	}
	// Assignment must not grant deletion ownership for the externally created disk.
	apiRequest("DELETE", volumeBase+"/"+legacyVolumeID, nil, 409)
	t.Log("namespace removal/re-addition and CLI dry-run/apply/resume retained IDs, templates, assignments and task history")
	apiRequest("DELETE", volumeBase+"/"+disk.ID, nil, 409)
	apiCommand("DELETE", apiVMPath+"/disks/"+disk.ID, nil)
	apiCommand("DELETE", volumeBase+"/"+disk.ID, nil)
	apiCommand("DELETE", volumeBase+"/"+clone.ID, nil)
	if imageURL == "" {
		apiRequest("DELETE", volumeBase+"/"+(compute.VolumeAddress{Host: "acceptance", Path: base}).ID(), nil, 404)
	} else {
		apiRequest("DELETE", volumeBase+"/"+(compute.VolumeAddress{Host: "acceptance", Path: base}).ID(), nil, 404)
	}
	apiRequest("POST", apiVMPath+"/interfaces", map[string]any{"network": "lan", "model": "virtio"}, 422)
	apiCommand("POST", apiVMPath+"/interfaces", map[string]any{"network": "lan", "mac": "52:54:00:ab:cd:01"})
	apiCommand("DELETE", apiVMPath+"/interfaces/52:54:00:ab:cd:01", nil)
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	key := string(ssh.MarshalAuthorizedKey(pub))
	fp := ssh.FingerprintLegacyMD5(pub)
	apiRequest("POST", "/api/v1/keys", map[string]string{"public_key": key}, 201)
	apiRequest("GET", "/api/v1/keys/"+fp, nil, 200)
	selectedNamespace = "analytics"
	apiRequest("GET", "/api/v1/keys/"+fp, nil, 404)
	apiRequest("POST", "/api/v1/keys", map[string]string{"public_key": key}, 201)
	selectedNamespace = "platform"
	apiRequest("POST", "/api/v1/keys", map[string]string{"public_key": key}, 409)
	apiRequest("DELETE", "/api/v1/keys/"+fp, nil, 204)
	apiRequest("GET", "/api/v1/keys/"+fp, nil, 404)
	selectedNamespace = "analytics"
	apiRequest("GET", "/api/v1/keys/"+fp, nil, 200)
	apiRequest("DELETE", "/api/v1/keys/"+fp, nil, 204)
	selectedNamespace = "platform"
	apiCommand("DELETE", apiVMPath+"?delete_volumes=true", nil)
	if _, err := vir("dominfo", domainUUID); err == nil {
		t.Fatal("VM still exists after HTTP delete")
	}
	var deletedDomain libvirtxml.Domain
	if err := deletedDomain.Unmarshal(xml); err != nil {
		t.Fatal(err)
	}
	for _, d := range deletedDomain.Devices.Disks {
		if d.Source == nil || d.Source.File == nil {
			continue
		}
		file := d.Source.File.File
		if _, err := vir("vol-info", "--pool", name, file); err == nil {
			t.Fatalf("volume remains after HTTP delete: %s (%v)", file, err)
		}
	}
	t.Log("PASS: create → start → VNC/serial → guest cloud-init → poweroff → delete VM and disks")
}

// These settings describe what the test must observe. They do not change the
// application's backend choice or weaken the guest boot and console assertions.
func acceptanceRuntimeOptions(defaultType, requestedType, requestedTimeout string) (string, time.Duration, error) {
	virtualization := defaultType
	if requestedType != "" {
		switch requestedType {
		case "kvm", "hvf", "qemu":
			virtualization = requestedType
		default:
			return "", 0, fmt.Errorf("ACCEPTANCE_VIRT_TYPE must be kvm, hvf or qemu")
		}
	}
	bootTimeout := 5 * time.Minute
	if requestedTimeout != "" {
		var err error
		bootTimeout, err = time.ParseDuration(requestedTimeout)
		if err != nil || bootTimeout < time.Second || bootTimeout > 20*time.Minute {
			return "", 0, fmt.Errorf("ACCEPTANCE_BOOT_TIMEOUT must be a duration between 1s and 20m")
		}
	}
	return virtualization, bootTimeout, nil
}

func TestAcceptanceRuntimeOptions(t *testing.T) {
	for _, tt := range []struct {
		defaultType, requestedType, timeout, wantType string
		wantTimeout                                   time.Duration
		invalid                                       bool
	}{
		{"kvm", "", "", "kvm", 5 * time.Minute, false},
		{"hvf", "", "", "hvf", 5 * time.Minute, false},
		{"kvm", "qemu", "20m", "qemu", 20 * time.Minute, false},
		{"kvm", "other", "", "", 0, true},
		{"kvm", "qemu", "21m", "", 0, true},
		{"kvm", "qemu", "0s", "", 0, true},
		{"kvm", "qemu", "invalid", "", 0, true},
	} {
		got, timeout, err := acceptanceRuntimeOptions(tt.defaultType, tt.requestedType, tt.timeout)
		if tt.invalid {
			if err == nil {
				t.Fatalf("accepted invalid options: %+v", tt)
			}
			continue
		}
		if err != nil || got != tt.wantType || timeout != tt.wantTimeout {
			t.Fatalf("options %+v: %s %s %v", tt, got, timeout, err)
		}
	}
}
