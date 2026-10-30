//go:build acceptance

package acceptance

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
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
	expectedType := "kvm"
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
	write(filepath.Join(root, "resources.txt"), "pool="+name+"\npool_uuid="+poolUUID+"\npool_path="+poolDir+"\n")
	t.Cleanup(func() {
		// A connection error is not evidence that the domain is gone. Never
		// remove its storage unless domain absence/undefine was confirmed.
		domains, err := vir("list", "--all", "--name")
		if err != nil {
			t.Errorf("cleanup cannot list domains: %s %v", domains, err)
			return
		}
		exists := false
		for _, domain := range strings.Fields(domains) {
			if domain == name {
				exists = true
			}
		}
		if exists {
			xml, err := vir("dumpxml", name)
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
			if err := encodingxml.Unmarshal([]byte(xml), &ownership); err != nil || len(ownership.Disks) == 0 || (domainUUID != "" && domainUUID != ownership.UUID) {
				t.Errorf("refusing cleanup: cannot verify test domain ownership")
				return
			}
			for _, disk := range ownership.Disks {
				if disk.Source.Dev != "" || !strings.HasPrefix(disk.Source.File, poolDir+string(os.PathSeparator)) {
					t.Errorf("refusing cleanup: domain has storage outside the test pool")
					return
				}
			}
			state, err := vir("domstate", name)
			if err != nil {
				t.Errorf("cleanup cannot get domain state: %v", err)
				return
			}
			if strings.TrimSpace(state) != "shut off" {
				if out, err := vir("destroy", name); err != nil {
					t.Errorf("cleanup cannot stop domain: %s %v", out, err)
					return
				}
			}
			if out, err := vir("undefine", name, "--nvram"); err != nil {
				t.Errorf("cleanup domain: %s %v", out, err)
				return
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
libvirt "acceptance" {
 uri = %q
 config_drive_pool = %q
 image_pool = %q
 qcow_prealloc_metadata = false
}
image "boot" { path = %q os_name = "Ubuntu" os_arch = %q firmware = %q protected = true disk_bus = "scsi" disk_controller = "virtio-scsi" guest_agent = false initialization = %q minimum_memory_mib = 1024 minimum_disk_gib = 8 }
image "no-init" { path = %q os_name = "Ubuntu" os_arch = %q firmware = %q initialization = "none" }
tasks { directory = %q workers = 2 }
api {
 listen = %q
 jwt_signing_key = %q
 console_signing_key = %q
 session_dir = %q
 token_dir = %q
 user "acceptance" { hashed_password = %q }
}
`, filepath.Join(root, "keys"), uri, name, name, base, arch, firmware, initialization, base, arch, firmware, filepath.Join(root, "tasks"), address, uuid.NewString()+uuid.NewString(), uuid.NewString()+uuid.NewString(), filepath.Join(root, "sessions"), filepath.Join(root, "tokens"), string(hash))

	configuration += fmt.Sprintf(`
cloud "acceptance" {
 images=["boot","no-init"]
 zone "test" {}
 instance_type "small" { cpu=2 memory_mib=2048 boot_disk_gib=8 }
 instance_type "too-small" { cpu=2 memory_mib=512 boot_disk_gib=8 }
 instance_type "larger" { cpu=2 memory_mib=3072 boot_disk_gib=16 }
 network "lan" {}
 storage_class "standard" {}
 host "acceptance" { zone="test" labels={rack="test"} network "lan" { network="qemu-usernet" } storage_class "standard" {pool=%q format="qcow2"} }
}
`, name)
	if imageURL != "" {
		if strings.Contains(imageURL, "alpinelinux.org/") {
			configuration = strings.ReplaceAll(configuration, `os_name = "Ubuntu"`, `os_name = "Alpine Linux"`)
		}
		configuration = strings.ReplaceAll(configuration, fmt.Sprintf("path = %q", base), fmt.Sprintf("url = %q sha256 = %q source_format = \"qcow2\"", imageURL, imageSHA))
		if sourceBytes > 0 {
			configuration = strings.ReplaceAll(configuration, `source_format = "qcow2"`, fmt.Sprintf(`source_format = "qcow2" source_size_bytes = %d`, sourceBytes))
		}
	}
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
		if method != "GET" && (strings.Contains(path, "/nodes/") || strings.Contains(path, "/clouds/")) && !strings.Contains(path, "/consoles/") {
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
		if response.StatusCode != status {
			t.Fatalf("%s %s: %d, want %d: %s", method, path, response.StatusCode, status, body)
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
							return apiRequest("GET", "/api/v1/nodes/acceptance/volumes/"+base64.RawURLEncoding.EncodeToString([]byte(effect.Resource.ID)), nil, 200)
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
	for _, resource := range []string{"clouds", "clouds/acceptance", "clouds/acceptance/machines", "nodes", "nodes/acceptance", "nodes/acceptance/images", "nodes/acceptance/pools", "nodes/acceptance/networks", "nodes/acceptance/capabilities", "keys", "vms", "volumes", "networks"} {
		apiRequest("GET", "/api/v1/"+resource, nil, 200)
	}
	if imageURL != "" {
		if _, err := vir("vol-info", base); err == nil {
			t.Fatal("catalog listing downloaded an image")
		}
		var catalog []struct {
			ID    string `json:"id"`
			State string `json:"state"`
		}
		json.Unmarshal(apiRequest("GET", "/api/v1/nodes/acceptance/images", nil, 200), &catalog)
		if len(catalog) != 2 || catalog[0].State != "downloadable" {
			t.Fatalf("missing managed image unavailable: %+v", catalog)
		}
	}
	request("/machines/add/?cloud=acceptance", 200)
	marker := "VMANGO_BOOT_OK_" + uuid.NewString()
	serialDevice := "/dev/ttyS0"
	if arch == "aarch64" {
		serialDevice = "/dev/ttyAMA0"
	}
	userdata := fmt.Sprintf("#cloud-config\nruncmd:\n  - [sh, -c, 'test \"$(hostname)\" = %s && echo %s > %s']\n", name, marker, serialDevice)
	// Check image constraints and the no-init alias through the same JSON API.
	aliasBody := map[string]any{"id": name, "image": "boot", "instance_type": "too-small", "zone": "test", "network": "lan", "storage_class": "standard", "start": false}
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
	apiCommand("POST", "/api/v1/clouds/acceptance/machines", aliasBody)
	if imageURL != "" {
		if _, err := vir("vol-info", base); err != nil {
			t.Fatal("verified image missing", err)
		}
	}
	domainUUID = strings.TrimSpace(mustVir("domuuid", name))
	noInitXML := mustVir("dumpxml", name)
	if strings.Contains(noInitXML, "_config.iso") || !strings.Contains(noInitXML, "org.qemu.guest_agent.0") {
		t.Fatal("no-init alias defaults were not applied")
	}

	apiCommand("POST", "/api/v1/clouds/acceptance/machines/acceptance/"+name+"/instance-type", map[string]any{"instance_type": "larger"})
	changedXML := mustVir("dumpxml", name)
	if !strings.Contains(changedXML, "3145728") {
		t.Fatal("instance type RAM change missing from domain XML")
	}
	info := mustVir("vol-info", "--pool", name, name+"_root", "--bytes")
	if !strings.Contains(info, "8589934592") {
		t.Fatal("instance type change resized the boot disk", info)
	}
	// Simulate an unmanaged domain without changing any resources or claiming disks.
	mustVir("metadata", name, "--config", "--uri", "https://vmango.org/xmlns/scheduler/1", "--remove")
	var observed struct {
		Managed bool `json:"managed"`
	}
	json.Unmarshal(apiRequest("GET", "/api/v1/nodes/acceptance/vms/"+name, nil, 200), &observed)
	if observed.Managed {
		t.Fatal("unmanaged machine was inferred from its name")
	}
	apiRequest("POST", "/api/v1/nodes/acceptance/vms/"+name+"/actions", map[string]string{"action": "start"}, 409)
	apiCommand("POST", "/api/v1/clouds/acceptance/machines/acceptance/"+name+"/adopt", map[string]any{})
	if !strings.Contains(mustVir("dumpxml", name), "3145728") {
		t.Fatal("adoption changed resource configuration")
	}
	apiCommand("DELETE", "/api/v1/nodes/acceptance/vms/"+name+"?delete_volumes=true", nil)
	domainUUID = ""
	apiVMPath := "/api/v1/nodes/acceptance/vms/" + name
	createBody := map[string]any{"id": name, "image": "boot", "instance_type": "small", "zone": "test", "network": "lan", "storage_class": "standard", "start": false, "user_data": userdata}
	apiCommand("POST", "/api/v1/clouds/acceptance/machines", createBody)
	// A duplicate name fails during the asynchronous fresh inventory stage.
	duplicate := apiRequest("POST", "/api/v1/clouds/acceptance/machines", createBody, 202)
	var duplicateAccepted struct {
		StatusURL string `json:"status_url"`
	}
	json.Unmarshal(duplicate, &duplicateAccepted)
	for deadline := time.Now().Add(10 * time.Second); ; {
		var task tasks.Task
		json.Unmarshal(apiRequest("GET", duplicateAccepted.StatusURL, nil, 200), &task)
		if task.State.Terminal() {
			if task.State != tasks.Failed || task.OutcomeUncertain {
				t.Fatalf("duplicate create: %+v", task)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("duplicate create did not fail")
		}
		time.Sleep(50 * time.Millisecond)
	}
	apiCommand("PATCH", apiVMPath, map[string]any{"autostart": false})
	domainUUID = strings.TrimSpace(mustVir("domuuid", name))
	write(filepath.Join(root, "resources.txt"), "pool="+name+"\npool_uuid="+poolUUID+"\npool_path="+poolDir+"\nvm_uuid="+domainUUID+"\n")
	path := "/machines/acceptance/" + name + "/"
	xml := mustVir("dumpxml", name)
	write(filepath.Join(root, "domain.xml"), xml)
	if !strings.Contains(xml, "type='"+expectedType+"'") {
		t.Fatalf("expected hardware accelerator %s; see domain.xml", expectedType)
	}
	if !strings.Contains(xml, "virtio-scsi") || strings.Contains(xml, "org.qemu.guest_agent.0") || !strings.Contains(xml, "_config.iso") {
		t.Fatal("image launch defaults did not reach domain XML")
	}
	request(path, 200)
	t.Log("created VM and cloud-init disk through HTTP; starting guest")
	apiCommand("POST", apiVMPath+"/actions", map[string]string{"action": "start"})
	if state := strings.TrimSpace(mustVir("domstate", name)); state != "running" {
		t.Fatalf("expected running, got %s", state)
	}
	headers := http.Header{"Origin": {origin}, "Authorization": {"Bearer " + access}}
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
	console.SetReadDeadline(time.Now().Add(5 * time.Minute))
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
	apiCommand("POST", apiVMPath+"/actions", map[string]string{"action": "poweroff"})
	if state := strings.TrimSpace(mustVir("domstate", name)); state != "shut off" {
		t.Fatalf("expected shut off, got %s", state)
	}
	// Exercise the remaining mutating routes only against this isolated VM/pool.
	volumeBase := "/api/v1/nodes/acceptance/volumes"
	var disk struct {
		ID string `json:"id"`
	}
	rawDisk := apiCommand("POST", "/api/v1/clouds/acceptance/volumes", map[string]any{"name": "extra", "zone": "test", "storage_class": "standard", "size_bytes": 16 << 20})
	if err := json.Unmarshal(rawDisk, &disk); err != nil {
		t.Fatal(err)
	}
	apiRequest("GET", volumeBase+"/"+disk.ID, nil, 200)
	apiCommand("POST", volumeBase+"/"+disk.ID+"/resize", map[string]any{"size_bytes": 32 << 20})
	var clone struct {
		ID string `json:"id"`
	}
	rawClone := apiCommand("POST", volumeBase+"/"+disk.ID+"/clone", map[string]any{"name": "extra-clone", "storage_class": "standard", "size_bytes": 32 << 20})
	if err := json.Unmarshal(rawClone, &clone); err != nil {
		t.Fatal(err)
	}
	apiCommand("POST", apiVMPath+"/disks", map[string]any{"volume_id": disk.ID})
	apiRequest("DELETE", volumeBase+"/"+disk.ID, nil, 409)
	apiCommand("DELETE", apiVMPath+"/disks/"+disk.ID, nil)
	apiCommand("DELETE", volumeBase+"/"+disk.ID, nil)
	apiCommand("DELETE", volumeBase+"/"+clone.ID, nil)
	if imageURL == "" {
		apiRequest("DELETE", volumeBase+"/"+base64.RawURLEncoding.EncodeToString([]byte(base)), nil, 409)
	} else {
		apiRequest("DELETE", volumeBase+"/"+base64.RawURLEncoding.EncodeToString([]byte(base)), nil, 404)
	}
	apiCommand("POST", apiVMPath+"/interfaces", map[string]any{"network": "lan", "mac": "52:54:00:ab:cd:01", "model": "virtio"})
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
	apiRequest("POST", "/api/v1/keys", map[string]string{"public_key": key}, 409)
	apiRequest("DELETE", "/api/v1/keys/"+fp, nil, 204)
	apiRequest("GET", "/api/v1/keys/"+fp, nil, 404)
	apiCommand("DELETE", apiVMPath+"?delete_volumes=true", nil)
	if _, err := vir("dominfo", name); err == nil {
		t.Fatal("VM still exists after HTTP delete")
	}
	for _, file := range []string{name + "_root", name + "_config.iso"} {
		if _, err := vir("vol-info", "--pool", name, file); err == nil {
			t.Fatalf("volume remains after HTTP delete: %s (%v)", file, err)
		}
	}
	t.Log("PASS: create → start → VNC/serial → guest cloud-init → poweroff → delete VM and disks")
}
