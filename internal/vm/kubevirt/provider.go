// Package kubevirt provides a VMProvider backed by KubeVirt.
//
// VMs are represented as VirtualMachine custom resources in a Kubernetes
// cluster that has KubeVirt installed.  Communication is done via plain HTTPS
// calls to the Kubernetes API server so that no external client library is
// required.
//
// Import this package with a blank identifier to register the provider:
//
//	import _ "github.com/chxmxii/kimo/internal/vm/kubevirt"
package kubevirt

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/chxmxii/kimo/internal/config"
	"github.com/chxmxii/kimo/internal/vm"
)

func init() {
	vm.Register("kubevirt", func(cfg interface{}) (vm.Provider, error) {
		c, ok := cfg.(config.KubeVirtConfig)
		if !ok {
			return nil, fmt.Errorf("kubevirt: expected config.KubeVirtConfig, got %T", cfg)
		}
		return New(c)
	})
}

const (
	kvAPIGroup   = "kubevirt.io"
	kvAPIVersion = "v1"
	kvResource   = "virtualmachines"
)

// Provider implements vm.Provider using KubeVirt VirtualMachine CRs.
type Provider struct {
	cfg       config.KubeVirtConfig
	apiServer string
	token     string
	client    *http.Client
}

// New creates a new KubeVirt Provider.
func New(cfg config.KubeVirtConfig) (*Provider, error) {
	apiServer := cfg.APIServerURL
	token := cfg.BearerToken

	// If a kubeconfig path is provided, read the in-cluster token / CA from
	// well-known paths (simplified: we parse only the bearer token and server).
	if cfg.KubeConfigPath != "" {
		ks, err := loadKubeConfig(cfg.KubeConfigPath)
		if err != nil {
			return nil, fmt.Errorf("kubevirt: loading kubeconfig: %w", err)
		}
		if apiServer == "" {
			apiServer = ks.server
		}
		if token == "" {
			token = ks.token
		}
	}

	if apiServer == "" {
		return nil, fmt.Errorf("kubevirt: api_server_url is required")
	}

	//nolint:gosec // Skipping TLS verification is opt-in for development setups.
	tlsCfg := &tls.Config{InsecureSkipVerify: false}
	httpClient := &http.Client{
		Timeout:   30 * time.Second,
		Transport: &http.Transport{TLSClientConfig: tlsCfg},
	}

	return &Provider{
		cfg:       cfg,
		apiServer: apiServer,
		token:     token,
		client:    httpClient,
	}, nil
}

// Name returns "kubevirt".
func (p *Provider) Name() string { return "kubevirt" }

// Validate checks connectivity to the Kubernetes API server.
func (p *Provider) Validate(ctx context.Context) error {
	url := fmt.Sprintf("%s/apis/%s/%s/namespaces/%s/%s?limit=1",
		p.apiServer, kvAPIGroup, kvAPIVersion, p.cfg.Namespace, kvResource)
	req, err := p.newRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("kubevirt: validate: %w", err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("kubevirt: validate: connecting to %s: %w", p.apiServer, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("kubevirt: validate: unauthorized – check bearer token")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("kubevirt: validate: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// CreateVM creates a KubeVirt VirtualMachine CR and returns its instance handle.
func (p *Provider) CreateVM(ctx context.Context, spec vm.Spec) (*vm.Instance, error) {
	id, err := randomID()
	if err != nil {
		return nil, fmt.Errorf("kubevirt: generating id: %w", err)
	}

	name := "kimo-" + id
	labels := map[string]string{"app": "kimo"}
	for k, v := range spec.Tags {
		labels[k] = v
	}

	memMB := spec.MemoryMB
	if memMB == 0 {
		memMB = 256
	}
	cpus := spec.CPUs
	if cpus == 0 {
		cpus = 1
	}
	image := spec.Image
	if image == "" {
		image = "quay.io/kubevirt/cirros-container-disk-demo:latest"
	}

	vmCR := buildVMCR(name, p.cfg.Namespace, image, cpus, memMB, labels)
	body, err := json.Marshal(vmCR)
	if err != nil {
		return nil, fmt.Errorf("kubevirt: marshalling VM CR: %w", err)
	}

	url := fmt.Sprintf("%s/apis/%s/%s/namespaces/%s/%s",
		p.apiServer, kvAPIGroup, kvAPIVersion, p.cfg.Namespace, kvResource)
	req, err := p.newRequest(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("kubevirt: CreateVM: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("kubevirt: CreateVM: status %d: %s", resp.StatusCode, string(raw))
	}

	return &vm.Instance{
		ID:        name,
		Status:    vm.StatusStarting,
		CreatedAt: time.Now(),
		Spec:      spec,
	}, nil
}

// DeleteVM deletes the KubeVirt VirtualMachine CR.
func (p *Provider) DeleteVM(ctx context.Context, instanceID string) error {
	url := fmt.Sprintf("%s/apis/%s/%s/namespaces/%s/%s/%s",
		p.apiServer, kvAPIGroup, kvAPIVersion, p.cfg.Namespace, kvResource, instanceID)
	req, err := p.newRequest(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("kubevirt: DeleteVM: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("kubevirt: instance %q not found", instanceID)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("kubevirt: DeleteVM: status %d: %s", resp.StatusCode, string(raw))
	}
	return nil
}

// GetVM fetches the current state of a KubeVirt VirtualMachine CR.
func (p *Provider) GetVM(ctx context.Context, instanceID string) (*vm.Instance, error) {
	url := fmt.Sprintf("%s/apis/%s/%s/namespaces/%s/%s/%s",
		p.apiServer, kvAPIGroup, kvAPIVersion, p.cfg.Namespace, kvResource, instanceID)
	req, err := p.newRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("kubevirt: GetVM: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("kubevirt: instance %q not found", instanceID)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kubevirt: GetVM: status %d", resp.StatusCode)
	}

	var cr vmCR
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return nil, fmt.Errorf("kubevirt: GetVM: decoding response: %w", err)
	}

	status := cr.kimoStatus()
	return &vm.Instance{
		ID:     instanceID,
		Status: status,
	}, nil
}

// ListVMs lists all KubeVirt VirtualMachine CRs in the configured namespace
// that were created by kimo.
func (p *Provider) ListVMs(ctx context.Context) ([]*vm.Instance, error) {
	url := fmt.Sprintf("%s/apis/%s/%s/namespaces/%s/%s?labelSelector=app%%3Dkimo",
		p.apiServer, kvAPIGroup, kvAPIVersion, p.cfg.Namespace, kvResource)
	req, err := p.newRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("kubevirt: ListVMs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kubevirt: ListVMs: status %d", resp.StatusCode)
	}

	var list struct {
		Items []vmCR `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("kubevirt: ListVMs: decoding response: %w", err)
	}

	result := make([]*vm.Instance, 0, len(list.Items))
	for _, cr := range list.Items {
		result = append(result, &vm.Instance{
			ID:     cr.Metadata.Name,
			Status: cr.kimoStatus(),
		})
	}
	return result, nil
}

// ---- internal types ---------------------------------------------------------

type vmCR struct {
	Metadata struct {
		Name      string            `json:"name"`
		Namespace string            `json:"namespace"`
		Labels    map[string]string `json:"labels"`
	} `json:"metadata"`
	Status struct {
		Ready bool   `json:"ready"`
		Phase string `json:"phase"`
	} `json:"status"`
}

func (cr *vmCR) kimoStatus() vm.Status {
	if cr.Status.Ready {
		return vm.StatusRunning
	}
	switch cr.Status.Phase {
	case "Scheduling", "Pending":
		return vm.StatusPending
	case "Running":
		return vm.StatusRunning
	case "Stopped", "Succeeded":
		return vm.StatusStopped
	case "Failed", "Unknown":
		return vm.StatusError
	}
	return vm.StatusPending
}

// buildVMCR returns the VirtualMachine CR object for the given parameters.
func buildVMCR(name, namespace, image string, cpus, memMB int, labels map[string]string) map[string]interface{} {
	return map[string]interface{}{
		"apiVersion": fmt.Sprintf("%s/%s", kvAPIGroup, kvAPIVersion),
		"kind":       "VirtualMachine",
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": namespace,
			"labels":    labels,
		},
		"spec": map[string]interface{}{
			"running": true,
			"template": map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": labels,
				},
				"spec": map[string]interface{}{
					"domain": map[string]interface{}{
						"cpu": map[string]interface{}{
							"cores": cpus,
						},
						"resources": map[string]interface{}{
							"requests": map[string]interface{}{
								"memory": fmt.Sprintf("%dMi", memMB),
							},
						},
						"devices": map[string]interface{}{
							"disks": []interface{}{
								map[string]interface{}{
									"name": "containerdisk",
									"disk": map[string]interface{}{"bus": "virtio"},
								},
							},
						},
					},
					"volumes": []interface{}{
						map[string]interface{}{
							"name": "containerdisk",
							"containerDisk": map[string]interface{}{
								"image": image,
							},
						},
					},
				},
			},
		},
	}
}

// ---- kubeconfig parsing -----------------------------------------------------

type kubeConfigData struct {
	server string
	token  string
}

// loadKubeConfig reads a minimal kubeconfig file and extracts the API server
// URL and bearer token for the current-context.  This is intentionally a slim
// parser; for production use, prefer k8s.io/client-go.
func loadKubeConfig(path string) (*kubeConfigData, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Parse only the fields kimo needs.
	var kc struct {
		CurrentContext string `json:"current-context"`
		Clusters       []struct {
			Name    string `json:"name"`
			Cluster struct {
				Server string `json:"server"`
			} `json:"cluster"`
		} `json:"clusters"`
		Users []struct {
			Name string `json:"name"`
			User struct {
				Token string `json:"token"`
			} `json:"user"`
		} `json:"users"`
		Contexts []struct {
			Name    string `json:"name"`
			Context struct {
				Cluster string `json:"cluster"`
				User    string `json:"user"`
			} `json:"context"`
		} `json:"contexts"`
	}

	// kubeconfig files are YAML; parse as JSON after a best-effort conversion.
	// For simplicity we rely on encoding/json and accept YAML-subset-that-is-JSON.
	// A full YAML parse would require an external library.
	if err := json.Unmarshal(raw, &kc); err != nil {
		return nil, fmt.Errorf("parsing kubeconfig (must be JSON-compatible YAML): %w", err)
	}

	// Find the current context.
	var clusterName, userName string
	for _, ctx := range kc.Contexts {
		if ctx.Name == kc.CurrentContext {
			clusterName = ctx.Context.Cluster
			userName = ctx.Context.User
			break
		}
	}

	var server string
	for _, cl := range kc.Clusters {
		if cl.Name == clusterName {
			server = cl.Cluster.Server
			break
		}
	}

	var token string
	for _, u := range kc.Users {
		if u.Name == userName {
			token = u.User.Token
			break
		}
	}

	return &kubeConfigData{server: server, token: token}, nil
}

// ---- helpers ----------------------------------------------------------------

func (p *Provider) newRequest(ctx context.Context, method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func randomID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", b), nil
}
