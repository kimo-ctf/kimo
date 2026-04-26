// Package kubevirt_test validates the KubeVirt provider wiring.
package kubevirt_test

import (
	"testing"

	"github.com/chxmxii/kimo/internal/config"
	"github.com/chxmxii/kimo/internal/vm"

	// Trigger init() registration.
	_ "github.com/chxmxii/kimo/internal/vm/kubevirt"
)

func TestKubeVirtRegistered(t *testing.T) {
	found := false
	for _, n := range vm.Registered() {
		if n == "kubevirt" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("kubevirt provider not registered")
	}
}

func TestKubeVirtNew_WrongConfig(t *testing.T) {
	_, err := vm.New("kubevirt", "not-a-config")
	if err == nil {
		t.Error("expected error when passing wrong config type")
	}
}

func TestKubeVirtNew_MissingAPIServer(t *testing.T) {
	// No kubeconfig and no api_server_url → New() must fail.
	cfg := config.KubeVirtConfig{
		Namespace: "kimo",
	}
	_, err := vm.New("kubevirt", cfg)
	if err == nil {
		t.Error("expected error when api_server_url is missing")
	}
}

func TestKubeVirtNew_WithAPIServer(t *testing.T) {
	cfg := config.KubeVirtConfig{
		Namespace:    "kimo",
		APIServerURL: "https://k8s.example.com",
		BearerToken:  "test-token",
	}
	p, err := vm.New("kubevirt", cfg)
	if err != nil {
		t.Fatalf("New(kubevirt): %v", err)
	}
	if p.Name() != "kubevirt" {
		t.Errorf("Name(): got %q, want \"kubevirt\"", p.Name())
	}
}
