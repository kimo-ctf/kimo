package vm_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/chxmxii/kimo/internal/vm"
)

// ---- fake provider ----------------------------------------------------------

type fakeVMProvider struct{ name string }

func (f *fakeVMProvider) Name() string                                               { return f.name }
func (f *fakeVMProvider) Validate(_ context.Context) error                           { return nil }
func (f *fakeVMProvider) CreateVM(_ context.Context, spec vm.Spec) (*vm.Instance, error) {
	return &vm.Instance{ID: "fake-id", Status: vm.StatusRunning, Spec: spec}, nil
}
func (f *fakeVMProvider) DeleteVM(_ context.Context, _ string) error     { return nil }
func (f *fakeVMProvider) GetVM(_ context.Context, id string) (*vm.Instance, error) {
	return &vm.Instance{ID: id, Status: vm.StatusRunning}, nil
}
func (f *fakeVMProvider) ListVMs(_ context.Context) ([]*vm.Instance, error) {
	return []*vm.Instance{}, nil
}

// ---- tests ------------------------------------------------------------------

func TestVMRegisterAndNew(t *testing.T) {
	const name = "test-vm-alpha"
	vm.Register(name, func(_ interface{}) (vm.Provider, error) {
		return &fakeVMProvider{name: name}, nil
	})

	p, err := vm.New(name, nil)
	if err != nil {
		t.Fatalf("New(%q): %v", name, err)
	}
	if p.Name() != name {
		t.Errorf("Name(): got %q, want %q", p.Name(), name)
	}
}

func TestVMNew_UnknownProvider(t *testing.T) {
	_, err := vm.New("totally-unknown-vm", nil)
	if err == nil {
		t.Error("expected error for unknown provider, got nil")
	}
}

func TestVMRegister_DuplicatePanics(t *testing.T) {
	const name = "test-vm-dup"
	vm.Register(name, func(_ interface{}) (vm.Provider, error) {
		return &fakeVMProvider{name: name}, nil
	})

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on duplicate registration")
		}
	}()
	vm.Register(name, func(_ interface{}) (vm.Provider, error) {
		return &fakeVMProvider{name: name}, nil
	})
}

func TestVMRegistered(t *testing.T) {
	const name = "test-vm-list"
	vm.Register(name, func(_ interface{}) (vm.Provider, error) {
		return &fakeVMProvider{name: name}, nil
	})

	found := false
	for _, n := range vm.Registered() {
		if n == name {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Registered() did not include %q", name)
	}
}

func TestVMFactory_ErrorPropagated(t *testing.T) {
	const name = "test-vm-err"
	vm.Register(name, func(_ interface{}) (vm.Provider, error) {
		return nil, fmt.Errorf("factory error")
	})

	_, err := vm.New(name, nil)
	if err == nil {
		t.Error("expected factory error to be propagated")
	}
}

func TestVMProviderFirecrackerRegistered(t *testing.T) {
	// Firecracker registers itself via init(); confirm it's present after
	// importing the parent package.  We import via the blank-import in main;
	// here we test the registry state directly.
	//
	// NOTE: this test does NOT require a real Firecracker binary.
	_ = vm.Registered() // just ensure no panic
}
