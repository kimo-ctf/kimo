// Package instance ties together the platform provider, VM provider, and
// proof-of-work manager to implement the core instance lifecycle.
package instance

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/chxmxii/kimo/internal/platform"
	"github.com/chxmxii/kimo/internal/pow"
	"github.com/chxmxii/kimo/internal/vm"
)

// Instance represents a running challenge VM instance.
type Instance struct {
	ID             string       `json:"id"`
	TeamID         string       `json:"team_id"`
	CTFChallengeID string       `json:"ctf_challenge_id"`
	VM             *vm.Instance `json:"vm"`
	CreatedAt      time.Time    `json:"created_at"`
}

// CreateRequest is the payload needed to start a new instance.
type CreateRequest struct {
	TeamID         string  `json:"team_id"`
	TeamToken      string  `json:"team_token"`
	CTFChallengeID string  `json:"ctf_challenge_id"`
	PoWChallengeID string  `json:"pow_challenge_id"`
	PoWNonce       string  `json:"pow_nonce"`
	VMSpec         vm.Spec `json:"vm_spec"`
}

// Manager manages the lifecycle of challenge instances.
type Manager struct {
	platform platform.Provider
	vm       vm.Provider
	pow      *pow.Manager

	mu        sync.RWMutex
	instances map[string]*Instance
}

// New returns a Manager wired with the given providers.
func New(p platform.Provider, v vm.Provider, pw *pow.Manager) *Manager {
	return &Manager{
		platform:  p,
		vm:        v,
		pow:       pw,
		instances: make(map[string]*Instance),
	}
}

// IssuePoW creates and returns a new PoW challenge that the client must solve
// before it can create or delete an instance.
func (m *Manager) IssuePoW() (*pow.Challenge, error) {
	return m.pow.Issue()
}

// Create validates the PoW solution and team credentials, verifies the CTF
// challenge exists, then starts a VM and records the instance.
func (m *Manager) Create(ctx context.Context, req CreateRequest) (*Instance, error) {
	// 1. Verify the PoW first so unauthenticated callers can't abuse the API.
	if err := m.pow.Verify(req.PoWChallengeID, req.PoWNonce); err != nil {
		return nil, fmt.Errorf("instance: PoW verification failed: %w", err)
	}

	// 2. Validate team credentials against the CTF platform.
	if err := m.platform.ValidateAccess(ctx, req.TeamID, req.TeamToken); err != nil {
		return nil, fmt.Errorf("instance: team validation failed: %w", err)
	}

	// 3. Confirm the challenge exists on the platform.
	if _, err := m.platform.GetChallenge(ctx, req.CTFChallengeID); err != nil {
		return nil, fmt.Errorf("instance: challenge lookup failed: %w", err)
	}

	// 4. Provision the VM.
	vmInst, err := m.vm.CreateVM(ctx, req.VMSpec)
	if err != nil {
		return nil, fmt.Errorf("instance: VM creation failed: %w", err)
	}

	inst := &Instance{
		ID:             vmInst.ID,
		TeamID:         req.TeamID,
		CTFChallengeID: req.CTFChallengeID,
		VM:             vmInst,
		CreatedAt:      time.Now(),
	}

	m.mu.Lock()
	m.instances[inst.ID] = inst
	m.mu.Unlock()

	return inst, nil
}

// Delete verifies the PoW solution and terminates the instance with the given ID.
func (m *Manager) Delete(ctx context.Context, instanceID, powChallengeID, powNonce string) error {
	if err := m.pow.Verify(powChallengeID, powNonce); err != nil {
		return fmt.Errorf("instance: PoW verification failed: %w", err)
	}

	m.mu.RLock()
	_, ok := m.instances[instanceID]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("instance: %q not found", instanceID)
	}

	if err := m.vm.DeleteVM(ctx, instanceID); err != nil {
		return fmt.Errorf("instance: deleting VM: %w", err)
	}

	m.mu.Lock()
	delete(m.instances, instanceID)
	m.mu.Unlock()

	return nil
}

// Get returns the current state of an instance.
func (m *Manager) Get(ctx context.Context, instanceID string) (*Instance, error) {
	m.mu.RLock()
	inst, ok := m.instances[instanceID]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("instance: %q not found", instanceID)
	}

	// Refresh VM status from the provider.
	vmInst, err := m.vm.GetVM(ctx, instanceID)
	if err != nil && !errors.Is(err, context.Canceled) {
		// Non-fatal: return cached state.
		return inst, nil
	}
	if vmInst != nil {
		m.mu.Lock()
		inst.VM = vmInst
		m.mu.Unlock()
	}

	return inst, nil
}

// List returns all tracked instances.
func (m *Manager) List(_ context.Context) []*Instance {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Instance, 0, len(m.instances))
	for _, inst := range m.instances {
		result = append(result, inst)
	}
	return result
}
