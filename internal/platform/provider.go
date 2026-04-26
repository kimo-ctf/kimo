// Package platform defines the PlatformProvider interface and the global
// provider registry used to select a CTF-platform backend at runtime.
package platform

import (
	"context"
	"fmt"
	"sync"
)

// Challenge represents a CTF challenge retrieved from the platform.
type Challenge struct {
	ID          string
	Name        string
	Description string
	Category    string
	Points      int
	Tags        []string
}

// Provider is the interface every CTF-platform backend must implement.
// Implementations are registered via Register and selected by name through
// New.  Adding a new platform requires only implementing this interface and
// calling Register in an init() function.
type Provider interface {
	// Name returns the unique identifier of this provider (e.g. "ctfd").
	Name() string

	// Validate checks that the provider is properly configured and that the
	// remote platform is reachable.
	Validate(ctx context.Context) error

	// GetChallenge fetches challenge metadata by its platform-side ID.
	GetChallenge(ctx context.Context, challengeID string) (*Challenge, error)

	// ValidateAccess checks that the given team / token pair is authorised on
	// the platform.
	ValidateAccess(ctx context.Context, teamID, token string) error
}

// Factory is the constructor signature for a platform provider.
// The config value is the platform-specific config struct (e.g. CTFdConfig).
type Factory func(cfg interface{}) (Provider, error)

var (
	mu       sync.RWMutex
	registry = make(map[string]Factory)
)

// Register makes a provider factory available under the given name.
// It is safe to call from multiple init() functions.
// Panics if the same name is registered twice.
func Register(name string, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := registry[name]; dup {
		panic(fmt.Sprintf("platform: provider %q already registered", name))
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
		return nil, fmt.Errorf("platform: unknown provider %q (did you import it?)", name)
	}
	return f(cfg)
}

// Registered returns the names of all registered providers (sorted for stability).
func Registered() []string {
	mu.RLock()
	defer mu.RUnlock()
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	return names
}
