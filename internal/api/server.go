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

// Package api implements the KIMO REST API server. Authentication is
// delegated entirely to the active integrations.Backend — the server
// never owns credentials of its own.
package api

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/hermannchristopher/kimo/internal/integrations"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// defaultNamespace is where KIMO's CRs live. A future task can make this
// configurable; every controller and handler currently agrees on "default".
const defaultNamespace = "default"

type Server struct {
	client    client.Client
	backend   integrations.Backend
	router    chi.Router
	namespace string

	powMu   sync.Mutex
	puzzles map[string]PoWPuzzle
}

func NewServer(c client.Client, backend integrations.Backend) *Server {
	s := &Server{
		client:    c,
		backend:   backend,
		namespace: defaultNamespace,
		puzzles:   map[string]PoWPuzzle{},
	}
	s.setupRoutes()
	return s
}

func (s *Server) Router() chi.Router { return s.router }

func (s *Server) setupRoutes() {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/api/v1/health", s.handleHealth)

	r.Group(func(r chi.Router) {
		r.Use(s.authMiddleware)
		r.Get("/api/v1/templates", s.handleListTemplates)
		r.Get("/api/v1/templates/{name}", s.handleGetTemplate)
		r.Post("/api/v1/instances", s.handleCreateInstance)
		r.Get("/api/v1/instances", s.handleListInstances)
		r.Get("/api/v1/instances/{name}", s.handleGetInstance)
		r.Delete("/api/v1/instances/{name}", s.handleDeleteInstance)
		r.Patch("/api/v1/instances/{name}/extend", s.handleExtendInstance)
		r.Get("/api/v1/pow/challenge", s.handlePoWChallenge)
		r.Post("/api/v1/webhooks/configure", s.handleConfigureWebhook)
	})

	s.router = r
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleConfigureWebhook only works when the active backend supports runtime
// webhook registration (today: the generic backend). Other backends return
// 501 — their integration is configured via Helm values instead.
func (s *Server) handleConfigureWebhook(w http.ResponseWriter, r *http.Request) {
	registrar, ok := s.backend.(integrations.WebhookRegistrar)
	if !ok {
		http.Error(w, "active scoring backend does not support webhook registration", http.StatusNotImplemented)
		return
	}
	var body struct {
		URL    string `json:"url"`
		Secret string `json:"secret"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if err := registrar.RegisterWebhook(body.URL, body.Secret); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
