/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

func init() {
	Register("ctfd", newCTFdBackend)
}

type ctfdConfig struct {
	BaseURL    string `json:"baseUrl"`    // CTFd instance, used for Authenticate
	WebhookURL string `json:"webhookUrl"` // where lifecycle events are POSTed; optional
	APIKey     string `json:"apiKey"`     // KIMO -> CTFd calls
}

type ctfdBackend struct {
	cfg    ctfdConfig
	client *http.Client
}

func newCTFdBackend(cfg json.RawMessage) (Backend, error) {
	var c ctfdConfig
	if err := json.Unmarshal(cfg, &c); err != nil {
		return nil, fmt.Errorf("parsing ctfd backend config: %w", err)
	}
	if c.BaseURL == "" {
		return nil, fmt.Errorf("ctfd backend requires baseUrl")
	}
	return &ctfdBackend{cfg: c, client: http.DefaultClient}, nil
}

func (b *ctfdBackend) Name() string { return "ctfd" }

type ctfdEventPayload struct {
	Type      string `json:"type"`
	Challenge string `json:"challenge_id"`
	Team      string `json:"team_id"`
	URL       string `json:"instance_url"`
}

func (b *ctfdBackend) Notify(ctx context.Context, event Event) error {
	if b.cfg.WebhookURL == "" {
		return nil // no CTFd webhook configured — Notify is a no-op
	}
	body, err := json.Marshal(ctfdEventPayload{
		Type:      string(event.Type),
		Challenge: event.Challenge,
		Team:      event.Team,
		URL:       event.Endpoint,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Token "+b.cfg.APIKey)

	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("ctfd webhook returned %d", resp.StatusCode)
	}
	return nil
}

// Authenticate validates the caller's CTFd API token by calling CTFd's own
// /api/v1/users/me — KIMO never stores or issues its own credentials for
// this backend, CTFd remains the source of truth.
func (b *ctfdBackend) Authenticate(r *http.Request) (Principal, error) {
	token := r.Header.Get("Authorization")
	if token == "" {
		return Principal{}, fmt.Errorf("missing authorization header")
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, b.cfg.BaseURL+"/api/v1/users/me", nil)
	if err != nil {
		return Principal{}, err
	}
	req.Header.Set("Authorization", token)

	resp, err := b.client.Do(req)
	if err != nil {
		return Principal{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Principal{}, fmt.Errorf("ctfd rejected credentials: %d", resp.StatusCode)
	}

	var body struct {
		Data struct {
			Name   string `json:"name"`
			TeamID int    `json:"team_id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Principal{}, err
	}
	return Principal{Subject: body.Data.Name, Team: fmt.Sprintf("%d", body.Data.TeamID)}, nil
}
