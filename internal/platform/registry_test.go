package platform_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/chxmxii/kimo/internal/platform"
)

// ---- fake provider ----------------------------------------------------------

type fakeProvider struct{ name string }

func (f *fakeProvider) Name() string                                               { return f.name }
func (f *fakeProvider) Validate(_ context.Context) error                           { return nil }
func (f *fakeProvider) GetChallenge(_ context.Context, id string) (*platform.Challenge, error) {
	return &platform.Challenge{ID: id}, nil
}
func (f *fakeProvider) ValidateAccess(_ context.Context, _, _ string) error { return nil }

// ---- tests ------------------------------------------------------------------

func TestRegisterAndNew(t *testing.T) {
	// Use a unique name to avoid conflicts with other tests.
	const name = "test-platform-alpha"
	platform.Register(name, func(cfg interface{}) (platform.Provider, error) {
		return &fakeProvider{name: name}, nil
	})

	p, err := platform.New(name, nil)
	if err != nil {
		t.Fatalf("New(%q): %v", name, err)
	}
	if p.Name() != name {
		t.Errorf("Name(): got %q, want %q", p.Name(), name)
	}
}

func TestNew_UnknownProvider(t *testing.T) {
	_, err := platform.New("totally-unknown-platform", nil)
	if err == nil {
		t.Error("expected error for unknown provider, got nil")
	}
}

func TestRegister_DuplicatePanics(t *testing.T) {
	const name = "test-platform-dup"
	platform.Register(name, func(_ interface{}) (platform.Provider, error) {
		return &fakeProvider{name: name}, nil
	})

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on duplicate registration")
		}
	}()
	platform.Register(name, func(_ interface{}) (platform.Provider, error) {
		return &fakeProvider{name: name}, nil
	})
}

func TestRegistered(t *testing.T) {
	const name = "test-platform-list"
	platform.Register(name, func(_ interface{}) (platform.Provider, error) {
		return &fakeProvider{name: name}, nil
	})

	found := false
	for _, n := range platform.Registered() {
		if n == name {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Registered() did not include %q", name)
	}
}

func TestFactory_ErrorPropagated(t *testing.T) {
	const name = "test-platform-err"
	platform.Register(name, func(_ interface{}) (platform.Provider, error) {
		return nil, fmt.Errorf("factory error")
	})

	_, err := platform.New(name, nil)
	if err == nil {
		t.Error("expected factory error to be propagated")
	}
}
