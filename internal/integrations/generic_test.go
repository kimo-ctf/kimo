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
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenericBackend_NotifySignsPayload(t *testing.T) {
	var gotSig, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.Header.Get("X-KIMO-Signature")
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		gotBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	b, err := newGenericBackend(nil)
	require.NoError(t, err)
	gb := b.(*genericBackend)
	gb.RegisterWebhook(srv.URL, "shared-secret")

	err = gb.Notify(context.Background(), Event{Type: EventRunning, Instance: "web-sqli-team-1"})
	require.NoError(t, err)

	mac := hmac.New(sha256.New, []byte("shared-secret"))
	mac.Write([]byte(gotBody))
	assert.Equal(t, hex.EncodeToString(mac.Sum(nil)), gotSig)
}

func TestGenericBackend_AuthenticateRejectsWrongKey(t *testing.T) {
	b, err := newGenericBackend(json.RawMessage(`{"apiKey":"secret-key"}`))
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer wrong-key")
	_, err = b.Authenticate(req)
	assert.Error(t, err)
}

func TestGenericBackend_AuthenticateAcceptsValidKey(t *testing.T) {
	b, err := newGenericBackend(json.RawMessage(`{"apiKey":"secret-key"}`))
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer secret-key")
	_, err = b.Authenticate(req)
	assert.NoError(t, err)
}
