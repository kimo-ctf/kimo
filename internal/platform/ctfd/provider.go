// Package ctfd provides a PlatformProvider implementation for CTFd.
//
// Import this package with a blank identifier to register the provider:
//
//	import _ "github.com/chxmxii/kimo/internal/platform/ctfd"
package ctfd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/chxmxii/kimo/internal/config"
	"github.com/chxmxii/kimo/internal/platform"
)

func init() {
	platform.Register("ctfd", func(cfg interface{}) (platform.Provider, error) {
		c, ok := cfg.(config.CTFdConfig)
		if !ok {
			return nil, fmt.Errorf("ctfd: expected config.CTFdConfig, got %T", cfg)
		}
		return New(c), nil
	})
}

// Provider implements platform.Provider for CTFd.
type Provider struct {
	cfg    config.CTFdConfig
	client *http.Client
}

// New returns a new CTFd Provider.
func New(cfg config.CTFdConfig) *Provider {
	return &Provider{
		cfg: cfg,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// Name returns the provider name.
func (p *Provider) Name() string { return "ctfd" }

// Validate checks that the CTFd instance is reachable and the API key is valid.
func (p *Provider) Validate(ctx context.Context) error {
	req, err := p.newRequest(ctx, http.MethodGet, "/api/v1/challenges?limit=1", nil)
	if err != nil {
		return fmt.Errorf("ctfd: validate: %w", err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("ctfd: validate: connecting to %s: %w", p.cfg.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("ctfd: validate: invalid API key")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ctfd: validate: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// GetChallenge fetches a challenge from CTFd by its numeric ID.
func (p *Provider) GetChallenge(ctx context.Context, challengeID string) (*platform.Challenge, error) {
	req, err := p.newRequest(ctx, http.MethodGet, "/api/v1/challenges/"+challengeID, nil)
	if err != nil {
		return nil, fmt.Errorf("ctfd: GetChallenge: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ctfd: GetChallenge: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("ctfd: challenge %q not found", challengeID)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ctfd: GetChallenge: unexpected status %d", resp.StatusCode)
	}

	var envelope struct {
		Success bool `json:"success"`
		Data    struct {
			ID          int      `json:"id"`
			Name        string   `json:"name"`
			Description string   `json:"description"`
			Category    string   `json:"category"`
			Value       int      `json:"value"`
			Tags        []string `json:"tags"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("ctfd: GetChallenge: decoding response: %w", err)
	}
	if !envelope.Success {
		return nil, fmt.Errorf("ctfd: GetChallenge: API returned success=false")
	}

	return &platform.Challenge{
		ID:          challengeID,
		Name:        envelope.Data.Name,
		Description: envelope.Data.Description,
		Category:    envelope.Data.Category,
		Points:      envelope.Data.Value,
		Tags:        envelope.Data.Tags,
	}, nil
}

// ValidateAccess checks team credentials against CTFd.
func (p *Provider) ValidateAccess(ctx context.Context, teamID, token string) error {
	req, err := p.newRequest(ctx, http.MethodGet, "/api/v1/teams/"+teamID, nil)
	if err != nil {
		return fmt.Errorf("ctfd: ValidateAccess: %w", err)
	}
	// Use the team-provided token for this request.
	req.Header.Set("Authorization", "Token "+token)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("ctfd: ValidateAccess: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("ctfd: ValidateAccess: unauthorized")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ctfd: ValidateAccess: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// newRequest constructs an authenticated HTTP request for the CTFd API.
func (p *Provider) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	url := p.cfg.URL + path
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Token "+p.cfg.APIKey)
	return req, nil
}
