// Package vm defines the VMProvider interface and the global provider registry
// used to select a VM backend at runtime.
package vm

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Status represents the lifecycle state of a virtual machine.
type Status string

const (
	StatusPending  Status = "pending"
	StatusStarting Status = "starting"
	StatusRunning  Status = "running"
	StatusStopped  Status = "stopped"
	StatusError    Status = "error"
)

// Spec describes the desired VM configuration.
type Spec struct {
	// Image is provider-specific: a rootfs path for Firecracker, a container
	// disk image for KubeVirt, etc.
	Image string
	// MemoryMB is the amount of RAM in megabytes.
	MemoryMB int
	// CPUs is the number of vCPUs.
	CPUs int
	// DiskGB is the size of the writable data disk in gigabytes (0 = default).
	DiskGB int
	// Tags are arbitrary key-value pairs propagated to the provider (e.g. k8s
	// labels).
	Tags map[string]string
}

// Instance is a live VM managed by a provider.
type Instance struct {
	// ID is the provider-assigned unique identifier.
	ID string
	// Status is the current lifecycle state.
	Status Status
	// IP is the primary reachable IP address (empty while pending).
	IP string
	// CreatedAt is when the instance was first created.
	CreatedAt time.Time
	// Spec is the configuration the instance was launched with.
	Spec Spec
}

// Provider is the interface every VM backend must implement.
// Backends are registered via Register and selected by name through New.
// Adding a new backend requires only implementing this interface and calling
// Register in an init() function.
type Provider interface {
	// Name returns the unique identifier of this provider (e.g. "firecracker").
	Name() string

	// Validate checks that the provider is properly configured and that any
	// required external services are reachable.
	Validate(ctx context.Context) error

	// CreateVM launches a new VM with the given specification and returns a
	// handle to the running instance.
	CreateVM(ctx context.Context, spec Spec) (*Instance, error)

	// DeleteVM terminates and removes the VM with the given ID.
	DeleteVM(ctx context.Context, instanceID string) error

	// GetVM returns the current state of the VM with the given ID.
	GetVM(ctx context.Context, instanceID string) (*Instance, error)

	// ListVMs returns all instances currently tracked by this provider.
	ListVMs(ctx context.Context) ([]*Instance, error)
}

// Factory is the constructor signature for a VM provider.
// The cfg value is the provider-specific config struct.
type Factory func(cfg interface{}) (Provider, error)

var (
	mu       sync.RWMutex
	registry = make(map[string]Factory)
)

// Register makes a provider factory available under the given name.
// Safe for concurrent use from multiple init() functions.
// Panics if the same name is registered twice.
func Register(name string, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := registry[name]; dup {
		panic(fmt.Sprintf("vm: provider %q already registered", name))
	}
	registry[name] = f
}

// New creates a provider by name using the registered factory.
// Returns an error if the name is unknown.
func New(name string, cfg interface{}) (Provider, error) {
	mu.RLock()
	f, ok := registry[name]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("vm: unknown provider %q (did you import it?)", name)
	}
	return f(cfg)
}

// Registered returns the names of all registered providers.
func Registered() []string {
	mu.RLock()
	defer mu.RUnlock()
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	return names
}
