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

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	kimov1alpha1 "github.com/hermannchristopher/kimo/api/v1alpha1"
	"github.com/hermannchristopher/kimo/internal/integrations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type stubAuthBackend struct{ apiKey string }

func (s *stubAuthBackend) Name() string { return "stub" }
func (s *stubAuthBackend) Notify(_ context.Context, _ integrations.Event) error {
	return nil
}
func (s *stubAuthBackend) Authenticate(r *http.Request) (integrations.Principal, error) {
	if r.Header.Get("Authorization") != "Bearer "+s.apiKey {
		return integrations.Principal{}, fmt.Errorf("unauthorized")
	}
	return integrations.Principal{Subject: "test"}, nil
}

func newTestAPIScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, kimov1alpha1.AddToScheme(s))
	require.NoError(t, corev1.AddToScheme(s))
	return s
}

func readyTemplate(name string) *kimov1alpha1.ChallengeTemplate {
	return &kimov1alpha1.ChallengeTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: defaultNamespace},
		Spec: kimov1alpha1.ChallengeTemplateSpec{
			FlagSecretRef: corev1.LocalObjectReference{Name: "flag"},
			InstanceMode:  kimov1alpha1.InstanceModePerTeam,
			TTL:           "30m",
			MaxInstances:  100,
			Container:     kimov1alpha1.ContainerSpec{Image: "ctf/web-sqli:v1"},
		},
		Status: kimov1alpha1.ChallengeTemplateStatus{Ready: true},
	}
}

func authedRequest(method, target string, body []byte) *http.Request {
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, target, bytes.NewReader(body))
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	req.Header.Set("Authorization", "Bearer secret-key")
	return req
}

func TestHealthEndpoint(t *testing.T) {
	srv := NewServer(nil, &stubAuthBackend{apiKey: "test-key"})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAuthMiddleware_RejectsUnauthenticated(t *testing.T) {
	srv := NewServer(nil, &stubAuthBackend{apiKey: "secret-key"})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/templates", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_AcceptsValidKey(t *testing.T) {
	scheme := newTestAPIScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	srv := NewServer(c, &stubAuthBackend{apiKey: "secret-key"})

	req := authedRequest(http.MethodGet, "/api/v1/templates", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.NotEqual(t, http.StatusUnauthorized, w.Code)
}

func TestCreateInstance_CreatesCR(t *testing.T) {
	scheme := newTestAPIScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(readyTemplate("web-sqli")).Build()
	srv := NewServer(c, &stubAuthBackend{apiKey: "secret-key"})

	body, _ := json.Marshal(map[string]string{"template": "web-sqli", "team": "team-1"})
	req := authedRequest(http.MethodPost, "/api/v1/instances", body)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code)

	var instance kimov1alpha1.ChallengeInstance
	require.NoError(t, c.Get(context.Background(),
		types.NamespacedName{Name: "web-sqli-team-1", Namespace: defaultNamespace}, &instance))
	assert.Equal(t, "web-sqli", instance.Spec.TemplateRef)
	assert.Equal(t, "team-1", instance.Spec.Team)
}

func TestCreateInstance_TemplateNotFound(t *testing.T) {
	scheme := newTestAPIScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	srv := NewServer(c, &stubAuthBackend{apiKey: "secret-key"})

	body, _ := json.Marshal(map[string]string{"template": "missing", "team": "team-1"})
	req := authedRequest(http.MethodPost, "/api/v1/instances", body)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestCreateInstance_RequiresPoWWhenEnabled(t *testing.T) {
	scheme := newTestAPIScheme(t)
	tmpl := readyTemplate("pow-challenge")
	tmpl.Spec.PoW = &kimov1alpha1.PoWSpec{Enabled: true, Difficulty: 8}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tmpl).Build()
	srv := NewServer(c, &stubAuthBackend{apiKey: "secret-key"})

	body, _ := json.Marshal(map[string]string{"template": "pow-challenge", "team": "team-1"})
	req := authedRequest(http.MethodPost, "/api/v1/instances", body)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusPreconditionRequired, w.Code)
}

func TestCreateInstance_SucceedsWithSolvedPoW(t *testing.T) {
	scheme := newTestAPIScheme(t)
	tmpl := readyTemplate("pow-challenge")
	tmpl.Spec.PoW = &kimov1alpha1.PoWSpec{Enabled: true, Difficulty: 8}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tmpl).Build()
	srv := NewServer(c, &stubAuthBackend{apiKey: "secret-key"})

	// Fetch a puzzle the same way a client would.
	req := authedRequest(http.MethodGet, "/api/v1/pow/challenge?template=pow-challenge", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var puzzle PoWPuzzle
	require.NoError(t, json.NewDecoder(w.Body).Decode(&puzzle))

	nonce, found := SolvePoW(puzzle.Challenge, puzzle.Difficulty)
	require.True(t, found)

	body, _ := json.Marshal(map[string]any{
		"template":     "pow-challenge",
		"team":         "team-1",
		"powChallenge": puzzle.Challenge,
		"powNonce":     nonce,
	})
	req = authedRequest(http.MethodPost, "/api/v1/instances", body)
	w = httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestListInstances_FiltersByTeam(t *testing.T) {
	scheme := newTestAPIScheme(t)
	instA := &kimov1alpha1.ChallengeInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name: "web-sqli-team-1", Namespace: defaultNamespace,
			Labels: map[string]string{"kimo.io/challenge": "web-sqli", "kimo.io/team": "team-1"},
		},
		Spec: kimov1alpha1.ChallengeInstanceSpec{TemplateRef: "web-sqli", Team: "team-1"},
	}
	instB := &kimov1alpha1.ChallengeInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name: "web-sqli-team-2", Namespace: defaultNamespace,
			Labels: map[string]string{"kimo.io/challenge": "web-sqli", "kimo.io/team": "team-2"},
		},
		Spec: kimov1alpha1.ChallengeInstanceSpec{TemplateRef: "web-sqli", Team: "team-2"},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instA, instB).Build()
	srv := NewServer(c, &stubAuthBackend{apiKey: "secret-key"})

	req := authedRequest(http.MethodGet, "/api/v1/instances?team=team-1", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var got []kimov1alpha1.ChallengeInstance
	require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
	require.Len(t, got, 1)
	assert.Equal(t, "web-sqli-team-1", got[0].Name)
}

func TestGetInstance_NotFound(t *testing.T) {
	scheme := newTestAPIScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	srv := NewServer(c, &stubAuthBackend{apiKey: "secret-key"})

	req := authedRequest(http.MethodGet, "/api/v1/instances/does-not-exist", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestDeleteInstance_RemovesCR(t *testing.T) {
	scheme := newTestAPIScheme(t)
	inst := &kimov1alpha1.ChallengeInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "web-sqli-team-1", Namespace: defaultNamespace},
		Spec:       kimov1alpha1.ChallengeInstanceSpec{TemplateRef: "web-sqli", Team: "team-1"},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(inst).Build()
	srv := NewServer(c, &stubAuthBackend{apiKey: "secret-key"})

	req := authedRequest(http.MethodDelete, "/api/v1/instances/web-sqli-team-1", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	require.Equal(t, http.StatusNoContent, w.Code)

	err := c.Get(context.Background(),
		types.NamespacedName{Name: "web-sqli-team-1", Namespace: defaultNamespace}, &kimov1alpha1.ChallengeInstance{})
	assert.Error(t, err)
}

func TestExtendInstance_UpdatesTTLOverride(t *testing.T) {
	scheme := newTestAPIScheme(t)
	inst := &kimov1alpha1.ChallengeInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "web-sqli-team-1", Namespace: defaultNamespace},
		Spec:       kimov1alpha1.ChallengeInstanceSpec{TemplateRef: "web-sqli", Team: "team-1"},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(inst).Build()
	srv := NewServer(c, &stubAuthBackend{apiKey: "secret-key"})

	body, _ := json.Marshal(map[string]string{"ttl": "45m"})
	req := authedRequest(http.MethodPatch, "/api/v1/instances/web-sqli-team-1/extend", body)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var updated kimov1alpha1.ChallengeInstance
	require.NoError(t, c.Get(context.Background(),
		types.NamespacedName{Name: "web-sqli-team-1", Namespace: defaultNamespace}, &updated))
	assert.Equal(t, "45m", updated.Spec.TTLOverride)
}

func TestListTemplates(t *testing.T) {
	scheme := newTestAPIScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(readyTemplate("web-sqli")).Build()
	srv := NewServer(c, &stubAuthBackend{apiKey: "secret-key"})

	req := authedRequest(http.MethodGet, "/api/v1/templates", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var got []kimov1alpha1.ChallengeTemplate
	require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
	assert.Len(t, got, 1)
}

func TestPoWChallenge_NotRequiredWhenTemplateHasNoPoW(t *testing.T) {
	scheme := newTestAPIScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(readyTemplate("web-sqli")).Build()
	srv := NewServer(c, &stubAuthBackend{apiKey: "secret-key"})

	req := authedRequest(http.MethodGet, "/api/v1/pow/challenge?template=web-sqli", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]bool
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.False(t, body["required"])
}

func TestConfigureWebhook_UnsupportedByBackend(t *testing.T) {
	srv := NewServer(nil, &stubAuthBackend{apiKey: "secret-key"})

	body, _ := json.Marshal(map[string]string{"url": "https://example.com/hook", "secret": "s"})
	req := authedRequest(http.MethodPost, "/api/v1/webhooks/configure", body)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotImplemented, w.Code)
}
