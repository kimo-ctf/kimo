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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCTFdBackend_NotifyPostsTranslatedPayload(t *testing.T) {
	var gotType string
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		gotType, _ = body["type"].(string)
		w.WriteHeader(http.StatusOK)
	}))
	defer webhook.Close()

	cfg, _ := json.Marshal(ctfdConfig{BaseURL: "https://ctfd.example.com", WebhookURL: webhook.URL, APIKey: "k"})
	b, err := newCTFdBackend(cfg)
	require.NoError(t, err)

	err = b.Notify(context.Background(), Event{Type: EventRunning, Challenge: "web-sqli", Team: "42"})
	require.NoError(t, err)
	assert.Equal(t, string(EventRunning), gotType)
}

func TestCTFdBackend_AuthenticateValidatesAgainstCTFd(t *testing.T) {
	ctfd := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Token good-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{"id": 1, "name": "team42", "team_id": 42},
		})
	}))
	defer ctfd.Close()

	cfg, _ := json.Marshal(ctfdConfig{BaseURL: ctfd.URL})
	b, err := newCTFdBackend(cfg)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Token good-token")
	principal, err := b.Authenticate(req)
	require.NoError(t, err)
	assert.Equal(t, "team42", principal.Subject)
	assert.Equal(t, "42", principal.Team)

	req.Header.Set("Authorization", "Token bad-token")
	_, err = b.Authenticate(req)
	assert.Error(t, err)
}

func TestCTFdBackend_RequiresBaseURL(t *testing.T) {
	_, err := newCTFdBackend(json.RawMessage(`{}`))
	assert.Error(t, err)
}
