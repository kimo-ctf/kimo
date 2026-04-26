// Package config provides configuration loading and validation for kimo.
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration for kimo.
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Platform PlatformConfig `yaml:"platform"`
	VM       VMConfig       `yaml:"vm"`
	PoW      PoWConfig      `yaml:"pow"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Port int    `yaml:"port"`
	Host string `yaml:"host"`
}

// PlatformConfig selects which CTF platform provider to use.
type PlatformConfig struct {
	// Type is the registered provider name, e.g. "ctfd".
	Type  string      `yaml:"type"`
	CTFd  CTFdConfig  `yaml:"ctfd"`
}

// CTFdConfig holds CTFd-specific settings.
type CTFdConfig struct {
	URL    string `yaml:"url"`
	APIKey string `yaml:"api_key"`
}

// VMConfig selects which VM backend provider to use.
type VMConfig struct {
	// Type is the registered provider name: "firecracker" or "kubevirt".
	Type        string             `yaml:"type"`
	Firecracker FirecrackerConfig  `yaml:"firecracker"`
	KubeVirt    KubeVirtConfig     `yaml:"kubevirt"`
}

// FirecrackerConfig holds Firecracker-specific settings.
type FirecrackerConfig struct {
	// BinaryPath is the path to the firecracker binary.
	BinaryPath string `yaml:"binary_path"`
	// SocketDir is the directory where per-VM API sockets are created.
	SocketDir string `yaml:"socket_dir"`
	// KernelImage is the path to the Linux kernel image (vmlinux).
	KernelImage string `yaml:"kernel_image"`
	// RootFSPath is the path to the base root filesystem image.
	RootFSPath string `yaml:"rootfs_path"`
	// CNINetworkName is the CNI network to attach VMs to (optional).
	CNINetworkName string `yaml:"cni_network_name"`
}

// KubeVirtConfig holds KubeVirt-specific settings.
type KubeVirtConfig struct {
	// KubeConfigPath is the path to a kubeconfig file.
	KubeConfigPath string `yaml:"kubeconfig"`
	// Namespace is the Kubernetes namespace where VMs are created.
	Namespace string `yaml:"namespace"`
	// APIServerURL overrides the API server URL from the kubeconfig (optional).
	APIServerURL string `yaml:"api_server_url"`
	// BearerToken is used if kubeconfig is not provided.
	BearerToken string `yaml:"bearer_token"`
}

// PoWConfig holds proof-of-work settings.
type PoWConfig struct {
	// Difficulty is the number of leading zero bits required in a valid solution.
	// Recommended: 20 (≈1M hash iterations on average).
	Difficulty int `yaml:"difficulty"`
	// TTL is the lifetime of an issued challenge.
	TTL time.Duration `yaml:"ttl"`
}

// Defaults applies sensible defaults to missing fields.
func (c *Config) Defaults() {
	if c.Server.Port == 0 {
		c.Server.Port = 8080
	}
	if c.Server.Host == "" {
		c.Server.Host = "0.0.0.0"
	}
	if c.Platform.Type == "" {
		c.Platform.Type = "ctfd"
	}
	if c.VM.Type == "" {
		c.VM.Type = "firecracker"
	}
	if c.PoW.Difficulty == 0 {
		c.PoW.Difficulty = 20
	}
	if c.PoW.TTL == 0 {
		c.PoW.TTL = 5 * time.Minute
	}
	if c.VM.Firecracker.BinaryPath == "" {
		c.VM.Firecracker.BinaryPath = "firecracker"
	}
	if c.VM.Firecracker.SocketDir == "" {
		c.VM.Firecracker.SocketDir = "/run/kimo/vms"
	}
	if c.VM.KubeVirt.Namespace == "" {
		c.VM.KubeVirt.Namespace = "kimo"
	}
}

// Validate returns an error if the configuration is incomplete or invalid.
func (c *Config) Validate() error {
	switch c.Platform.Type {
	case "ctfd":
		if c.Platform.CTFd.URL == "" {
			return fmt.Errorf("config: platform.ctfd.url is required")
		}
	default:
		return fmt.Errorf("config: unknown platform type %q", c.Platform.Type)
	}

	switch c.VM.Type {
	case "firecracker":
		if c.VM.Firecracker.KernelImage == "" {
			return fmt.Errorf("config: vm.firecracker.kernel_image is required")
		}
		if c.VM.Firecracker.RootFSPath == "" {
			return fmt.Errorf("config: vm.firecracker.rootfs_path is required")
		}
	case "kubevirt":
		// Either kubeconfig or bearer_token+api_server_url must be set.
		if c.VM.KubeVirt.KubeConfigPath == "" &&
			(c.VM.KubeVirt.BearerToken == "" || c.VM.KubeVirt.APIServerURL == "") {
			return fmt.Errorf("config: vm.kubevirt requires either kubeconfig or both api_server_url and bearer_token")
		}
	default:
		return fmt.Errorf("config: unknown vm type %q", c.VM.Type)
	}

	if c.PoW.Difficulty < 1 || c.PoW.Difficulty > 32 {
		return fmt.Errorf("config: pow.difficulty must be between 1 and 32 (got %d)", c.PoW.Difficulty)
	}
	if c.PoW.TTL < time.Second {
		return fmt.Errorf("config: pow.ttl must be at least 1s (got %s)", c.PoW.TTL)
	}

	return nil
}

// Load reads and parses a YAML config file from the given path.
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("config: opening %s: %w", path, err)
	}
	defer f.Close()

	var cfg Config
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("config: parsing %s: %w", path, err)
	}

	cfg.Defaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}
