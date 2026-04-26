// Package firecracker_test validates the Firecracker provider wiring.
package firecracker_test

import (
	"testing"

	"github.com/chxmxii/kimo/internal/config"
	"github.com/chxmxii/kimo/internal/vm"

	// Trigger init() registration.
	_ "github.com/chxmxii/kimo/internal/vm/firecracker"
)

func TestFirecrackerRegistered(t *testing.T) {
	found := false
	for _, n := range vm.Registered() {
		if n == "firecracker" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("firecracker provider not registered")
	}
}

func TestFirecrackerNew_WrongConfig(t *testing.T) {
	// Passing a non-FirecrackerConfig must return an error from the factory.
	_, err := vm.New("firecracker", "not-a-config")
	if err == nil {
		t.Error("expected error when passing wrong config type")
	}
}

func TestFirecrackerNew_ValidConfig(t *testing.T) {
	cfg := config.FirecrackerConfig{
		BinaryPath:  "/usr/bin/firecracker",
		SocketDir:   "/tmp/kimo-test",
		KernelImage: "/tmp/vmlinux",
		RootFSPath:  "/tmp/rootfs.ext4",
	}
	// New() itself must succeed; it does not check filesystem paths.
	p, err := vm.New("firecracker", cfg)
	if err != nil {
		t.Fatalf("New(firecracker): %v", err)
	}
	if p.Name() != "firecracker" {
		t.Errorf("Name(): got %q, want \"firecracker\"", p.Name())
	}
}
