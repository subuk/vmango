//go:build acceptance

package acceptance

import (
	"context"
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
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/bcrypt"
)

// TestLifecycle boots a real guest. Explicit opt-in and a local libvirt URI are
// required; this suite must never run as part of ordinary unit tests.
func TestLifecycle(t *testing.T) {
	image := os.Getenv("ACCEPTANCE_IMAGE")
	if image == "" {
		t.Fatal("set ACCEPTANCE_IMAGE to a local Ubuntu cloud image (see README.md)")
	}
	image, err := filepath.Abs(image)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(image); err != nil {
		t.Fatal(err)
	}
	uri := os.Getenv("ACCEPTANCE_URI")
	if uri == "" {
		uri = "qemu:///session"
	}
	if uri != "qemu:///session" && uri != "qemu:///system" {
		t.Fatal("only local qemu:///session or qemu:///system is supported")
	}
	arch := "x86_64"
	if runtime.GOARCH == "arm64" {
		arch = "aarch64"
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
	if runtime.GOOS == "darwin" {
		expectedType = "hvf"
		firmware = "efi"
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
	if err := os.Mkdir(poolDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Pool creation fails on collisions. Cleanup is limited to this random name.
	mustVir("pool-define-as", name, "dir", "--target", poolDir)
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
		vir("pool-destroy", name)
		if out, err := vir("pool-undefine", name); err != nil {
			t.Errorf("cleanup pool: %s %v", out, err)
			return
		}
		if err := os.RemoveAll(poolDir); err != nil {
			t.Error(err)
		}
	})
	mustVir("pool-start", name)
	base := filepath.Join(poolDir, "base.qcow2")
	if out, err := run("qemu-img", "convert", "-O", "qcow2", image, base); err != nil {
		t.Fatalf("prepare image: %s %v", out, err)
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
	write(config, fmt.Sprintf(`key_file = %q
libvirt "acceptance" {
 uri = %q
 config_drive_pool = %q
 qcow_prealloc_metadata = false
}
image "boot" { path = %q os_name = "Ubuntu" os_arch = %q firmware = %q protected = true disk_bus = "scsi" disk_controller = "virtio-scsi" guest_agent = false initialization = %q minimum_memory_mib = 1024 minimum_disk_gib = 8 }
image "no-init" { path = %q os_name = "Ubuntu" os_arch = %q firmware = %q initialization = "none" }
web {
 listen = %q
 session_secret = %q
 user "acceptance" { hashed_password = %q }
}
`, filepath.Join(root, "keys"), uri, name, base, arch, firmware, initialization, base, arch, firmware, address, uuid.NewString(), string(hash)))
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
	request := func(path string, values url.Values, status int) {
		t.Helper()
		var resp *http.Response
		var err error
		if values == nil {
			resp, err = client.Get(origin + path)
		} else {
			req, e := http.NewRequest("POST", origin+path, strings.NewReader(values.Encode()))
			if e != nil {
				t.Fatal(e)
			}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Origin", origin)
			resp, err = client.Do(req)
		}
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != status {
			t.Fatalf("%s: HTTP %d, want %d\n%s", path, resp.StatusCode, status, body)
		}
		if status == 302 && strings.HasPrefix(resp.Header.Get("Location"), "/login") {
			t.Fatal("unexpected authentication redirect")
		}
	}
	request("/login/", url.Values{"Username": {"acceptance"}, "Password": {password}}, 302)
	request("/machines/add/?node=acceptance&arch="+arch, nil, 200)
	request("/machines/add/?node=acceptance&mode=advanced&arch="+arch, nil, 200)
	marker := "VMANGO_BOOT_OK_" + uuid.NewString()
	userdata := fmt.Sprintf("#cloud-config\nruncmd:\n  - [sh, -c, 'test \"$(hostname)\" = %s && echo %s > /dev/console']\n", name, marker)
	values := url.Values{
		"Name": {name}, "NodeId": {"acceptance"}, "Arch": {arch}, "Vcpus": {"2"}, "MemoryValue": {"2"}, "MemoryUnit": {"G"},
		"GraphicType": {"vnc"}, "VideoModel": {"none"}, "Userdata": {userdata},
		"CloneImageID": {"boot"}, "CloneVolumeNewName": {"__magic_root_suffix__"}, "CloneVolumeNewPool": {name},
		"CloneVolumeNewFormat": {"qcow2"}, "CloneVolumeNewSizeValue": {"8"}, "CloneVolumeNewSizeUnit": {"G"},
		"CloneVolumeDeviceType": {"disk"}, "CloneVolumeDeviceBus": {""},
		"InterfaceNetwork": {"qemu-usernet"}, "InterfaceMac": {""}, "InterfaceModel": {""}, "InterfaceAccessVlan": {""},
	}
	// Resource validation must fail without leaving any destination disk.
	values.Set("MemoryValue", "512")
	values.Set("MemoryUnit", "M")
	request("/machines/add/", values, 400)
	if _, err := os.Stat(filepath.Join(poolDir, name+"_root")); !os.IsNotExist(err) {
		t.Fatal("invalid request created a disk")
	}
	values.Set("MemoryValue", "2")
	values.Set("MemoryUnit", "G")
	// The second catalog ID uses the same source with independent defaults.
	values.Set("CloneImageID", "no-init")
	request("/machines/add/", values, 302)
	domainUUID = strings.TrimSpace(mustVir("domuuid", name))
	noInitXML := mustVir("dumpxml", name)
	if strings.Contains(noInitXML, "_config.iso") || !strings.Contains(noInitXML, "org.qemu.guest_agent.0") {
		t.Fatal("no-init alias defaults were not applied")
	}
	request("/machines/acceptance/"+name+"/delete/", url.Values{"DeleteVolumes": {"true"}}, 302)
	values.Set("CloneImageID", "boot")
	domainUUID = ""
	request("/machines/add/", values, 302)
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
	request(path, nil, 200)
	t.Log("created VM and cloud-init disk through HTTP; starting guest")
	request(path+"set-state/start/", url.Values{}, 302)
	if state := strings.TrimSpace(mustVir("domstate", name)); state != "running" {
		t.Fatalf("expected running, got %s", state)
	}
	headers := http.Header{"Origin": {origin}}
	u, _ := url.Parse(origin)
	for _, c := range jar.Cookies(u) {
		headers.Add("Cookie", c.String())
	}
	dial := func(suffix string) *websocket.Conn {
		t.Helper()
		conn, resp, err := websocket.DefaultDialer.Dial("ws://"+address+path+suffix, headers)
		if err != nil {
			if resp != nil {
				resp.Body.Close()
			}
			t.Fatal(err)
		}
		return conn
	}
	console := dial("console-ws/")
	defer console.Close()
	console.SetReadDeadline(time.Now().Add(5 * time.Minute))
	consoleLog, err := os.Create(filepath.Join(root, "console.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer consoleLog.Close()
	vnc := dial("vnc/ws/")
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
	request(path+"set-state/poweroff/", url.Values{}, 302)
	if state := strings.TrimSpace(mustVir("domstate", name)); state != "shut off" {
		t.Fatalf("expected shut off, got %s", state)
	}
	request(path+"delete/", url.Values{"DeleteVolumes": {"true"}}, 302)
	if _, err := vir("dominfo", name); err == nil {
		t.Fatal("VM still exists after HTTP delete")
	}
	for _, file := range []string{name + "_root", name + "_config.iso"} {
		if _, err := os.Stat(filepath.Join(poolDir, file)); !os.IsNotExist(err) {
			t.Fatalf("volume remains after HTTP delete: %s (%v)", file, err)
		}
	}
	t.Log("PASS: create → start → VNC/serial → guest cloud-init → poweroff → delete VM and disks")
}
