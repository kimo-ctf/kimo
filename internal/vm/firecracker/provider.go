// Package firecracker provides a VMProvider backed by Firecracker microVMs.
//
// Each call to CreateVM spawns a new firecracker process with a dedicated Unix
// API socket, configures the VM (kernel, rootfs, resources), then starts it.
// The socket is placed under Config.SocketDir/<id>.sock.
//
// Import this package with a blank identifier to register the provider:
//
//	import _ "github.com/chxmxii/kimo/internal/vm/firecracker"
package firecracker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/chxmxii/kimo/internal/config"
	"github.com/chxmxii/kimo/internal/vm"
)

func init() {
	vm.Register("firecracker", func(cfg interface{}) (vm.Provider, error) {
		c, ok := cfg.(config.FirecrackerConfig)
		if !ok {
			return nil, fmt.Errorf("firecracker: expected config.FirecrackerConfig, got %T", cfg)
		}
		return New(c), nil
	})
}

// entry tracks a running Firecracker process.
type entry struct {
	instance *vm.Instance
	cmd      *exec.Cmd
	socket   string
}

// Provider implements vm.Provider using Firecracker.
type Provider struct {
	cfg config.FirecrackerConfig

	mu        sync.RWMutex
	instances map[string]*entry
}

// New creates a new Firecracker Provider.
func New(cfg config.FirecrackerConfig) *Provider {
	return &Provider{
		cfg:       cfg,
		instances: make(map[string]*entry),
	}
}

// Name returns "firecracker".
func (p *Provider) Name() string { return "firecracker" }

// Validate checks that the firecracker binary exists and the socket directory
// can be created.
func (p *Provider) Validate(_ context.Context) error {
	if _, err := exec.LookPath(p.cfg.BinaryPath); err != nil {
		return fmt.Errorf("firecracker: binary %q not found: %w", p.cfg.BinaryPath, err)
	}
	if err := os.MkdirAll(p.cfg.SocketDir, 0o750); err != nil {
		return fmt.Errorf("firecracker: creating socket dir %s: %w", p.cfg.SocketDir, err)
	}
	if _, err := os.Stat(p.cfg.KernelImage); err != nil {
		return fmt.Errorf("firecracker: kernel image %q: %w", p.cfg.KernelImage, err)
	}
	if _, err := os.Stat(p.cfg.RootFSPath); err != nil {
		return fmt.Errorf("firecracker: rootfs %q: %w", p.cfg.RootFSPath, err)
	}
	return nil
}

// CreateVM spawns a Firecracker process and configures + starts the microVM.
func (p *Provider) CreateVM(ctx context.Context, spec vm.Spec) (*vm.Instance, error) {
	id, err := randomID()
	if err != nil {
		return nil, fmt.Errorf("firecracker: generating id: %w", err)
	}

	socketPath := filepath.Join(p.cfg.SocketDir, id+".sock")

	// Spawn the Firecracker process.
	//nolint:gosec // binary path comes from trusted config.
	cmd := exec.CommandContext(ctx, p.cfg.BinaryPath, "--api-sock", socketPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("firecracker: starting process: %w", err)
	}

	// Wait for the socket to become available (up to 5 s).
	if err := waitForSocket(socketPath, 5*time.Second); err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("firecracker: waiting for API socket: %w", err)
	}

	client := socketClient(socketPath)

	memMB := spec.MemoryMB
	if memMB == 0 {
		memMB = 256
	}
	cpus := spec.CPUs
	if cpus == 0 {
		cpus = 1
	}

	// Configure machine resources.
	if err := fcPut(ctx, client, socketPath, "/machine-config", map[string]interface{}{
		"vcpu_count":   cpus,
		"mem_size_mib": memMB,
	}); err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("firecracker: machine-config: %w", err)
	}

	// Set the kernel boot source.
	if err := fcPut(ctx, client, socketPath, "/boot-source", map[string]interface{}{
		"kernel_image_path": p.cfg.KernelImage,
		"boot_args":         "console=ttyS0 reboot=k panic=1 pci=off",
	}); err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("firecracker: boot-source: %w", err)
	}

	// Attach the root filesystem.
	rootFS := p.cfg.RootFSPath
	if spec.Image != "" {
		rootFS = spec.Image
	}
	if err := fcPut(ctx, client, socketPath, "/drives/rootfs", map[string]interface{}{
		"drive_id":       "rootfs",
		"path_on_host":   rootFS,
		"is_root_device": true,
		"is_read_only":   false,
	}); err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("firecracker: drives/rootfs: %w", err)
	}

	// Start the VM.
	if err := fcPut(ctx, client, socketPath, "/actions", map[string]interface{}{
		"action_type": "InstanceStart",
	}); err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("firecracker: InstanceStart: %w", err)
	}

	inst := &vm.Instance{
		ID:        id,
		Status:    vm.StatusRunning,
		CreatedAt: time.Now(),
		Spec:      spec,
	}

	p.mu.Lock()
	p.instances[id] = &entry{instance: inst, cmd: cmd, socket: socketPath}
	p.mu.Unlock()

	return inst, nil
}

// DeleteVM terminates the Firecracker process and cleans up the socket.
func (p *Provider) DeleteVM(_ context.Context, instanceID string) error {
	p.mu.Lock()
	e, ok := p.instances[instanceID]
	if ok {
		delete(p.instances, instanceID)
	}
	p.mu.Unlock()

	if !ok {
		return fmt.Errorf("firecracker: instance %q not found", instanceID)
	}

	if e.cmd != nil && e.cmd.Process != nil {
		if err := e.cmd.Process.Kill(); err != nil {
			return fmt.Errorf("firecracker: killing process for %s: %w", instanceID, err)
		}
	}
	_ = os.Remove(e.socket)
	return nil
}

// GetVM returns the current state of the VM.
func (p *Provider) GetVM(_ context.Context, instanceID string) (*vm.Instance, error) {
	p.mu.RLock()
	e, ok := p.instances[instanceID]
	p.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("firecracker: instance %q not found", instanceID)
	}

	// Check if the process is still running.
	status := vm.StatusRunning
	if e.cmd != nil && e.cmd.ProcessState != nil && e.cmd.ProcessState.Exited() {
		status = vm.StatusStopped
	}
	e.instance.Status = status
	return e.instance, nil
}

// ListVMs returns all tracked instances.
func (p *Provider) ListVMs(_ context.Context) ([]*vm.Instance, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make([]*vm.Instance, 0, len(p.instances))
	for _, e := range p.instances {
		result = append(result, e.instance)
	}
	return result, nil
}

// ---- helpers ----------------------------------------------------------------

// socketClient returns an *http.Client that dials the given Unix socket.
func socketClient(socketPath string) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				d := net.Dialer{}
				return d.DialContext(ctx, "unix", socketPath)
			},
		},
		Timeout: 10 * time.Second,
	}
}

// fcPut sends a PUT request to the Firecracker API.
func fcPut(ctx context.Context, client *http.Client, socketPath, path string, body interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	// Firecracker's HTTP API listens on a Unix socket; the host in the URL is
	// ignored.
	url := "http://localhost" + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	_ = socketPath // already captured in the transport

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		var msg interface{}
		_ = json.NewDecoder(resp.Body).Decode(&msg)
		return fmt.Errorf("status %d: %v", resp.StatusCode, msg)
	}
	return nil
}

// waitForSocket polls until the given Unix socket file appears or the timeout
// expires.
func waitForSocket(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("socket %s did not appear within %s", path, timeout)
}

// randomID returns a cryptographically random 8-byte hex string suitable for
// use as an instance ID.
func randomID() (string, error) {
	b := make([]byte, 8)
	_, err := cryptoRead(b)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", b), nil
}
