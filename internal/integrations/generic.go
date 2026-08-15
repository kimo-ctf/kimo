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
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"sync"
)

func init() {
	Register("generic", newGenericBackend)
}

type genericConfig struct {
	APIKey string `json:"apiKey"`
}

// genericBackend is the zero-config default: HMAC-signed webhook fan-out
// and a static Bearer API key.
type genericBackend struct {
	apiKey string
	mu     sync.RWMutex
	hooks  map[string]string // url -> hmac secret
}

func newGenericBackend(cfg json.RawMessage) (Backend, error) {
	var c genericConfig
	if len(cfg) > 0 {
		if err := json.Unmarshal(cfg, &c); err != nil {
			return nil, fmt.Errorf("parsing generic backend config: %w", err)
		}
	}
	return &genericBackend{apiKey: c.APIKey, hooks: map[string]string{}}, nil
}

func (b *genericBackend) Name() string { return "generic" }

// RegisterWebhook implements integrations.WebhookRegistrar, letting the API
// server's /webhooks/configure endpoint stay backend-agnostic.
func (b *genericBackend) RegisterWebhook(url, secret string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.hooks[url] = secret
	return nil
}

func (b *genericBackend) Notify(ctx context.Context, event Event) error {
	b.mu.RLock()
	hooks := make(map[string]string, len(b.hooks))
	maps.Copy(hooks, b.hooks)
	b.mu.RUnlock()

	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}

	var errs []error
	for url, secret := range hooks {
		if err := postSigned(ctx, url, secret, payload); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (b *genericBackend) Authenticate(r *http.Request) (Principal, error) {
	if r.Header.Get("Authorization") != "Bearer "+b.apiKey {
		return Principal{}, fmt.Errorf("unauthorized")
	}
	return Principal{Subject: "api-key", Scopes: []string{"admin"}}, nil
}

func postSigned(ctx context.Context, url, secret string, payload []byte) error {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	sig := hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-KIMO-Signature", sig)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook %s returned %d", url, resp.StatusCode)
	}
	return nil
}
