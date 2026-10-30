// Command console connects stdin/stdout to a serial console, or a local VNC
// viewer to Vmango's authenticated WebSocket proxy. It never contacts libvirt.
package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/term"
)

func main() {
	endpoint := flag.String("url", "", "Vmango WebSocket URL")
	tokenFile := flag.String("token-file", "", "Protected file containing a personal API JWT")
	listen := flag.String("listen", "", "Optional loopback TCP address for a native VNC viewer, e.g. 127.0.0.1:5901")
	flag.Parse()
	if err := run(*endpoint, *tokenFile, *listen); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(endpoint, tokenFile, listen string) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return err
	}
	if u.Scheme != "wss" && !(u.Scheme == "ws" && (u.Hostname() == "localhost" || net.ParseIP(u.Hostname()).IsLoopback())) {
		return fmt.Errorf("use wss, except for loopback development")
	}
	if tokenFile == "" {
		return fmt.Errorf("-token-file is required; create a personal API token in the account menu")
	}
	raw, err := os.ReadFile(tokenFile)
	if err != nil {
		return fmt.Errorf("cannot read token file")
	}
	token := strings.TrimSpace(string(raw))
	if token == "" || strings.ContainsAny(token, "\r\n ") {
		return fmt.Errorf("invalid token file")
	}
	var input io.Reader = os.Stdin
	var output io.Writer = os.Stdout
	var local net.Conn
	if listen != "" {
		host, _, err := net.SplitHostPort(listen)
		if err != nil || !net.ParseIP(host).IsLoopback() {
			return fmt.Errorf("-listen must use a literal loopback IP address")
		}
		listener, err := net.Listen("tcp", listen)
		if err != nil {
			return err
		}
		defer listener.Close()
		fmt.Fprintln(os.Stderr, "Connect your VNC viewer to", listen)
		local, err = listener.Accept()
		if err != nil {
			return err
		}
		defer local.Close()
		input = local
		output = local
	}
	header := http.Header{"Authorization": {"Bearer " + token}}
	dialer := websocket.Dialer{Subprotocols: []string{"binary"}, HandshakeTimeout: 15 * time.Second}
	ws, response, err := dialer.Dial(endpoint, header)
	if err != nil {
		if response != nil {
			defer response.Body.Close()
			return fmt.Errorf("console handshake: HTTP %d", response.StatusCode)
		}
		return fmt.Errorf("console handshake failed")
	}
	defer ws.Close()
	ws.SetReadLimit(1 << 20)
	var once sync.Once
	closeBoth := func() {
		once.Do(func() {
			ws.Close()
			if local != nil {
				local.Close()
			}
		})
	}
	defer closeBoth()
	if listen == "" && term.IsTerminal(int(os.Stdin.Fd())) {
		old, err := term.MakeRaw(int(os.Stdin.Fd()))
		if err != nil {
			return err
		}
		defer term.Restore(int(os.Stdin.Fd()), old)
	}
	go func() {
		defer closeBoth()
		buf := make([]byte, 32<<10)
		for {
			n, err := input.Read(buf)
			if n > 0 {
				ws.SetWriteDeadline(time.Now().Add(15 * time.Second))
				if ws.WriteMessage(websocket.BinaryMessage, buf[:n]) != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	for {
		kind, data, err := ws.ReadMessage()
		if err != nil {
			return nil
		}
		if kind != websocket.BinaryMessage {
			return fmt.Errorf("unexpected non-binary frame")
		}
		if _, err = io.Copy(output, strings.NewReader(string(data))); err != nil {
			return err
		}
	}
}
