//go:build acceptance

package acceptance

import (
	"encoding/binary"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	x "libvirt.org/go/libvirtxml"
)

func presentationProfile(mode string) string {
	switch mode {
	case "headless":
		return `graphics { type="none" }`
	case "vnc":
		return `graphics {type="vnc" video_model="virtio"} input {keyboard="usb" pointer="usb-tablet"}`
	case "vmvga":
		return `graphics {type="vnc" video_model="vmvga"} input {keyboard="usb" pointer="usb-mouse"}`
	}
	return `graphics {type="vnc" video_model="none"}`
}
func checkPresentationXML(t *testing.T, c *x.Domain, mode string) {
	t.Helper()
	usb, inputs, videos := 0, 0, 0
	for _, ctl := range c.Devices.Controllers {
		if ctl.Type == "usb" && ctl.Model != "none" {
			usb++
			if ctl.Model != "qemu-xhci" {
				t.Fatal("unexpected USB model", ctl.Model)
			}
		}
	}
	for _, in := range c.Devices.Inputs {
		if in.Bus == "usb" {
			inputs++
			if in.Address == nil || in.Address.USB == nil {
				t.Fatal("USB address missing")
			}
		}
	}
	for _, v := range c.Devices.Videos {
		if v.Model.Type != "none" {
			videos++
			want := "virtio"
			if mode == "vmvga" {
				want = "vmvga"
			}
			if v.Model.Type != want {
				t.Fatal("video model mismatch", v.Model.Type)
			}
		}
	}
	if mode == "headless" {
		if usb != 0 || inputs != 0 || videos != 0 || len(c.Devices.Graphics) != 0 {
			t.Fatal("headless acquired display/USB devices")
		}
	} else if usb != 1 || inputs != 2 || videos != 1 || len(c.Devices.Graphics) != 1 {
		t.Fatal("graphics topology mismatch")
	}
	if len(c.Devices.Consoles) == 0 {
		t.Fatal("serial console removed")
	}
}

const guestNetworkProbe = `import socket, time, urllib.request
for attempt in range(6):
    try:
        addresses = socket.getaddrinfo('example.com', 80)
        with urllib.request.urlopen('http://example.com', timeout=10) as response:
            assert response.status == 200
            assert response.read(256)
        print('VMANGO_NETWORK_DNS_HTTP_OK', flush=True)
        break
    except Exception as error:
        print('VMANGO_NETWORK_RETRY', str(error), flush=True)
        if attempt == 5:
            raise
        time.sleep(2)
`

const guestInputProbe = `import glob, os, select, struct, time
fds = {}
end = time.time()+60
while len(fds)<2 and time.time()<end:
    for path in glob.glob('/sys/class/input/event*/device/name'):
        name=open(path).read().strip()
        kind='KEY' if 'QEMU USB Keyboard' in name else 'POINTER' if 'QEMU USB Tablet' in name or 'QEMU USB Mouse' in name else ''
        if kind and kind not in fds.values():
            event=path.split('/')[4]
            fds[os.open('/dev/input/'+event,os.O_RDONLY|os.O_NONBLOCK)]=kind
    time.sleep(.1)
assert len(fds)==2, fds
print('VMANGO_INPUT_READY',flush=True)
while fds and time.time()<end:
    for fd in select.select(list(fds),[],[],1)[0]:
        data=os.read(fd,struct.calcsize('@llHHI'))
        sec,usec,typ,code,value=struct.unpack('@llHHI',data)
        kind=fds[fd]
        if (kind=='KEY' and typ==1 and code==30 and value==1) or (kind=='POINTER' and typ in (2,3) and code==0 and value):
            print('VMANGO_INPUT_'+kind,flush=True)
            os.close(fd)
            del fds[fd]
assert not fds, fds
`

func presentationUserdata(mode, password, hostname, marker, serial string) string {
	script := "write_files:\n  - path: /var/lib/cloud/vmango-network.py\n    permissions: '0700'\n    content: |\n"
	for _, line := range strings.Split(guestNetworkProbe, "\n") {
		script += "      " + line + "\n"
	}
	command := fmt.Sprintf("; python3 /var/lib/cloud/vmango-network.py > %s 2>&1", serial)
	if mode != "headless" {
		script += "  - path: /var/lib/cloud/vmango-input.py\n    permissions: '0700'\n    content: |\n"
		for _, line := range strings.Split(guestInputProbe, "\n") {
			script += "      " + line + "\n"
		}
		command += fmt.Sprintf("; python3 /var/lib/cloud/vmango-input.py > %s 2>&1", serial)
	}
	return fmt.Sprintf("#cloud-config\n%sruncmd:\n  - [sh, -c, 'echo root:%s | chpasswd; test \"$(hostname)\" = %s && echo %s > %s%s']\n", script, password, hostname, marker, serial, command)
}

type websocketReader struct {
	c *websocket.Conn
	r io.Reader
}

func (w *websocketReader) Read(p []byte) (int, error) {
	for {
		if w.r == nil {
			_, r, e := w.c.NextReader()
			if e != nil {
				return 0, e
			}
			w.r = r
		}
		n, e := w.r.Read(p)
		if e == io.EOF {
			w.r = nil
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, e
	}
}

// Exercise the actual authenticated same-origin WebSocket transport using RFB,
// including guest-observed keyboard/pointer delivery, not just schema support.
func sendRFBInput(t *testing.T, c *websocket.Conn) {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(15 * time.Second))
	r := &websocketReader{c: c}
	read := func(n int) []byte {
		v := make([]byte, n)
		if _, e := io.ReadFull(r, v); e != nil {
			t.Fatal(e)
		}
		return v
	}
	write := func(v []byte) {
		if e := c.WriteMessage(websocket.BinaryMessage, v); e != nil {
			t.Fatal(e)
		}
	}
	greeting := read(12)
	if !strings.HasPrefix(string(greeting), "RFB ") {
		t.Fatal(string(greeting))
	}
	write([]byte("RFB 003.008\n"))
	count := int(read(1)[0])
	types := read(count)
	hasNone := false
	for _, v := range types {
		hasNone = hasNone || v == 1
	}
	if !hasNone {
		t.Fatal("internal RFB security unavailable", types)
	}
	write([]byte{1})
	if binary.BigEndian.Uint32(read(4)) != 0 {
		t.Fatal("RFB security rejected")
	}
	write([]byte{1})
	init := read(24)
	nameLen := int(binary.BigEndian.Uint32(init[20:]))
	if nameLen > 4096 {
		t.Fatal("invalid RFB name")
	}
	read(nameLen)
	width, height := binary.BigEndian.Uint16(init), binary.BigEndian.Uint16(init[2:])
	if width == 0 || height == 0 {
		t.Fatal("no framebuffer")
	}
	// Negotiate raw encoding before input, as real RFB clients do; QEMU
	// initializes the current absolute/relative pointer mode here.
	write([]byte{2, 0, 0, 1, 0, 0, 0, 0})
	key := []byte{4, 1, 0, 0, 0, 0, 0, 97}
	write(key)
	key[1] = 0
	write(key)
	write([]byte{5, 0, 0, 20, 0, 30})
	write([]byte{5, 0, 0, 120, 0, 130})
	write([]byte{3, 0, 0, 0, 0, 0, 0, 1, 0, 1})
	if read(4)[0] != 0 {
		t.Fatal("expected framebuffer update")
	}
	t.Logf("RFB framebuffer %dx%d; sent keyboard and pointer events", width, height)
}
func serialLogin(t *testing.T, c *websocket.Conn, password string) {
	t.Helper()
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(time.Minute))
	readUntil := func(needle string) {
		text := ""
		for !strings.Contains(text, needle) {
			_, b, e := c.ReadMessage()
			if e != nil {
				t.Fatalf("serial login missing %s: %v", needle, e)
			}
			text += string(b)
			if len(text) > 65536 {
				text = text[len(text)-65536:]
			}
		}
	}
	send := func(s string) {
		if e := c.WriteMessage(websocket.BinaryMessage, []byte(s)); e != nil {
			t.Fatal(e)
		}
	}
	// A newly opened serial stream can precede getty readiness. Keep waking
	// its prompt until observed; stop the writer before sending credentials.
	stop, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if c.WriteMessage(websocket.BinaryMessage, []byte("\r")) != nil {
					return
				}
			}
		}
	}()
	func() {
		defer close(stop)
		readUntil("login:")
	}()
	<-stopped
	send("root\r")
	readUntil("Password:")
	send(password + "\r")
	readUntil("#")
	send("printf 'VMANGO_SERIAL_%s\\n' LOGIN_OK\r")
	readUntil("VMANGO_SERIAL_LOGIN_OK")
	t.Log("headless guest serial login and shell command passed")
}
