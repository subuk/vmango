package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"subuk/vmango/compute"
	"subuk/vmango/config"
	"subuk/vmango/installer"
	"subuk/vmango/installer/richt"
	"subuk/vmango/libvirt"
)

// Install starts only the temporary installer, without initializing web credentials or stores.
func Install(configPath, binary, stateDir, listen string, parallelism int, out io.Writer) (err error) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return fmt.Errorf("install supports Linux and macOS controllers")
	}
	doc, err := config.OpenInstallDocument(configPath)
	if err != nil {
		return err
	}
	if stateDir == "" {
		base, e := os.UserCacheDir()
		if e != nil {
			return e
		}
		sum := sha256.Sum256([]byte(doc.Path()))
		stateDir = filepath.Join(base, "vmango", "install", hex.EncodeToString(sum[:])[:24])
	}
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("installer listen address must use a loopback IP")
	}
	provisioner, err := richt.New(richt.Options{Binary: binary, StateDir: filepath.Join(stateDir, "richt"), Parallelism: parallelism})
	if err != nil {
		return err
	}
	resources := newInstallResources(doc)
	defer func() { err = errors.Join(err, resources.Close()) }()
	controller, err := installer.New(installer.Options{Document: doc, StateDir: stateDir, Provisioner: provisioner, Parallelism: parallelism, Resources: resources.Get})
	if err != nil {
		return err
	}
	defer controller.Close()
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	handler, err := installer.NewHandler(controller, listener.Addr().String())
	if err != nil {
		return err
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	fmt.Fprintf(out, "Configuration: %s\nInstaller session: %s\n", doc.Path(), handler.SessionURL())
	fmt.Fprintf(out, "For a remote controller, open an SSH tunnel from your browser machine:\n  ssh -N -L %s:%s:%s USER@CONTROLLER\nKeep the same local port and open the session URL above.\n", port, host, port)
	fmt.Fprintln(out, "Closing the browser does not cancel approved work. Ctrl+C stops the installer; interrupted operations can be resumed explicitly. Vmango itself will not be started or restarted.")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case err := <-done:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
}

type installResourceEntry struct {
	pool    *libvirt.ConnectionPool
	service installer.Resources
}

// A session reuses a service for the same host-to-URI mapping regardless of
// form order. Pools are released only after the controller's workers stop.
type installResources struct {
	document *config.InstallDocument
	mu       sync.Mutex
	entries  map[string]installResourceEntry
	closed   bool
}

func newInstallResources(document *config.InstallDocument) *installResources {
	return &installResources{document: document, entries: map[string]installResourceEntry{}}
}

func (r *installResources) Get(hosts []installer.Host) installer.Resources {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return failedInstallResources{libvirt.ErrConnectionPoolClosed}
	}
	uriByHost := map[string]string{}
	var order []string
	for _, host := range hosts {
		uri, err := installer.ConnectionURI(r.document, host)
		if err != nil {
			return failedInstallResources{err}
		}
		if _, duplicate := uriByHost[host.ID]; duplicate {
			return failedInstallResources{fmt.Errorf("duplicate installer host %s", host.ID)}
		}
		uriByHost[host.ID] = uri
		order = append(order, host.ID)
	}
	sort.Strings(order)
	// JSON sorts string map keys; the opaque key keeps URI details out of logs.
	encoded, _ := json.Marshal(uriByHost)
	sum := sha256.Sum256(encoded)
	key := hex.EncodeToString(sum[:])
	if existing, ok := r.entries[key]; ok {
		return existing.service
	}
	pool := libvirt.NewConnectionPool(uriByHost, order, zerolog.Nop())
	service := compute.NewHostPreparationService(libvirt.NewHostPreparationRepository(pool))
	r.entries[key] = installResourceEntry{pool: pool, service: service}
	return service
}

func (r *installResources) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	var failures []error
	for _, entry := range r.entries {
		failures = append(failures, entry.pool.Close())
	}
	r.entries = nil
	return errors.Join(failures...)
}

type failedInstallResources struct{ err error }

func (f failedInstallResources) Prepare(context.Context, string, compute.HostPreparationSpec) (*compute.HostPreparationPlan, error) {
	return nil, f.err
}
func (f failedInstallResources) Apply(context.Context, *compute.HostPreparationPlan) error {
	return f.err
}
