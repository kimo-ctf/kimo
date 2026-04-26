package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chxmxii/kimo/internal/config"
)

func TestDefaults(t *testing.T) {
	cfg := &config.Config{}
	cfg.Defaults()

	if cfg.Server.Port != 8080 {
		t.Errorf("Server.Port: got %d, want 8080", cfg.Server.Port)
	}
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("Server.Host: got %q, want 0.0.0.0", cfg.Server.Host)
	}
	if cfg.Platform.Type != "ctfd" {
		t.Errorf("Platform.Type: got %q, want ctfd", cfg.Platform.Type)
	}
	if cfg.VM.Type != "firecracker" {
		t.Errorf("VM.Type: got %q, want firecracker", cfg.VM.Type)
	}
	if cfg.PoW.Difficulty != 20 {
		t.Errorf("PoW.Difficulty: got %d, want 20", cfg.PoW.Difficulty)
	}
	if cfg.PoW.TTL != 5*time.Minute {
		t.Errorf("PoW.TTL: got %s, want 5m", cfg.PoW.TTL)
	}
}

func TestValidate_CTFd_MissingURL(t *testing.T) {
	cfg := &config.Config{}
	cfg.Defaults()
	// ctfd URL missing → error
	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error for missing ctfd.url")
	}
}

func TestValidate_Firecracker_Valid(t *testing.T) {
	cfg := &config.Config{
		Platform: config.PlatformConfig{
			Type:  "ctfd",
			CTFd:  config.CTFdConfig{URL: "https://ctf.example.com"},
		},
		VM: config.VMConfig{
			Type: "firecracker",
			Firecracker: config.FirecrackerConfig{
				KernelImage: "/vmlinux",
				RootFSPath:  "/rootfs.ext4",
			},
		},
		PoW: config.PoWConfig{Difficulty: 20, TTL: 5 * time.Minute},
	}
	cfg.Defaults()
	if err := cfg.Validate(); err != nil {
		t.Errorf("unexpected validation error: %v", err)
	}
}

func TestValidate_Firecracker_MissingKernel(t *testing.T) {
	cfg := &config.Config{
		Platform: config.PlatformConfig{
			Type: "ctfd",
			CTFd: config.CTFdConfig{URL: "https://ctf.example.com"},
		},
		VM: config.VMConfig{
			Type:        "firecracker",
			Firecracker: config.FirecrackerConfig{RootFSPath: "/rootfs.ext4"},
		},
		PoW: config.PoWConfig{Difficulty: 20, TTL: 5 * time.Minute},
	}
	cfg.Defaults()
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for missing kernel_image")
	}
}

func TestValidate_KubeVirt_Valid(t *testing.T) {
	cfg := &config.Config{
		Platform: config.PlatformConfig{
			Type: "ctfd",
			CTFd: config.CTFdConfig{URL: "https://ctf.example.com"},
		},
		VM: config.VMConfig{
			Type: "kubevirt",
			KubeVirt: config.KubeVirtConfig{
				APIServerURL: "https://k8s.example.com",
				BearerToken:  "tok",
				Namespace:    "kimo",
			},
		},
		PoW: config.PoWConfig{Difficulty: 20, TTL: 5 * time.Minute},
	}
	cfg.Defaults()
	if err := cfg.Validate(); err != nil {
		t.Errorf("unexpected validation error: %v", err)
	}
}

func TestValidate_KubeVirt_MissingAPIServer(t *testing.T) {
	cfg := &config.Config{
		Platform: config.PlatformConfig{
			Type: "ctfd",
			CTFd: config.CTFdConfig{URL: "https://ctf.example.com"},
		},
		VM: config.VMConfig{
			Type:     "kubevirt",
			KubeVirt: config.KubeVirtConfig{Namespace: "kimo"},
		},
		PoW: config.PoWConfig{Difficulty: 20, TTL: 5 * time.Minute},
	}
	cfg.Defaults()
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for missing kubevirt api server / kubeconfig")
	}
}

func TestValidate_UnknownPlatform(t *testing.T) {
	cfg := &config.Config{
		Platform: config.PlatformConfig{Type: "unknown"},
		VM: config.VMConfig{
			Type:        "firecracker",
			Firecracker: config.FirecrackerConfig{KernelImage: "/k", RootFSPath: "/r"},
		},
		PoW: config.PoWConfig{Difficulty: 20, TTL: 5 * time.Minute},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for unknown platform type")
	}
}

func TestValidate_UnknownVM(t *testing.T) {
	cfg := &config.Config{
		Platform: config.PlatformConfig{
			Type: "ctfd",
			CTFd: config.CTFdConfig{URL: "https://ctf.example.com"},
		},
		VM:  config.VMConfig{Type: "unknown-vm"},
		PoW: config.PoWConfig{Difficulty: 20, TTL: 5 * time.Minute},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for unknown vm type")
	}
}

func TestValidate_PoW_InvalidDifficulty(t *testing.T) {
	cfg := &config.Config{
		Platform: config.PlatformConfig{
			Type: "ctfd",
			CTFd: config.CTFdConfig{URL: "https://ctf.example.com"},
		},
		VM: config.VMConfig{
			Type:        "firecracker",
			Firecracker: config.FirecrackerConfig{KernelImage: "/k", RootFSPath: "/r"},
		},
		PoW: config.PoWConfig{Difficulty: 0, TTL: 5 * time.Minute},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for difficulty=0")
	}
}

func TestLoad_ValidFile(t *testing.T) {
	content := `
server:
  port: 9090
  host: "127.0.0.1"
platform:
  type: ctfd
  ctfd:
    url: "https://ctf.example.com"
    api_key: "key"
vm:
  type: firecracker
  firecracker:
    kernel_image: "/vmlinux"
    rootfs_path: "/rootfs.ext4"
pow:
  difficulty: 10
  ttl: 2m
`
	tmp := filepath.Join(t.TempDir(), "kimo.yaml")
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(tmp)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("Port: got %d, want 9090", cfg.Server.Port)
	}
	if cfg.Platform.Type != "ctfd" {
		t.Errorf("Platform.Type: got %q", cfg.Platform.Type)
	}
	if cfg.PoW.Difficulty != 10 {
		t.Errorf("PoW.Difficulty: got %d, want 10", cfg.PoW.Difficulty)
	}
}

func TestLoad_NonExistentFile(t *testing.T) {
	_, err := config.Load("/nonexistent/kimo.yaml")
	if err == nil {
		t.Error("expected error for nonexistent config file")
	}
}
