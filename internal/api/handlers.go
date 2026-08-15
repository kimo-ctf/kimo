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
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	kimov1alpha1 "github.com/hermannchristopher/kimo/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Lifecycle notifications (instance.creating, instance.running, ...) are
// dispatched by the Instance and Lifecycle controllers as phase
// transitions happen. These handlers only mutate CRs — they never call
// Backend.Notify directly.

func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	var list kimov1alpha1.ChallengeTemplateList
	if err := s.client.List(r.Context(), &list, client.InNamespace(s.namespace)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, list.Items)
}

func (s *Server) handleGetTemplate(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var tmpl kimov1alpha1.ChallengeTemplate
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: name, Namespace: s.namespace}, &tmpl); err != nil {
		if errors.IsNotFound(err) {
			http.Error(w, "template not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, tmpl)
}

type createInstanceRequest struct {
	Template     string `json:"template"`
	Team         string `json:"team"`
	Player       string `json:"player,omitempty"`
	PoWChallenge string `json:"powChallenge,omitempty"`
	PoWNonce     uint64 `json:"powNonce,omitempty"`
}

func (s *Server) handleCreateInstance(w http.ResponseWriter, r *http.Request) {
	var body createInstanceRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if body.Template == "" || body.Team == "" {
		http.Error(w, "template and team are required", http.StatusBadRequest)
		return
	}

	var tmpl kimov1alpha1.ChallengeTemplate
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: body.Template, Namespace: s.namespace}, &tmpl); err != nil {
		if errors.IsNotFound(err) {
			http.Error(w, "template not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !tmpl.Status.Ready {
		http.Error(w, "template is not ready", http.StatusConflict)
		return
	}

	if tmpl.Spec.PoW != nil && tmpl.Spec.PoW.Enabled {
		if !s.consumePoW(body.PoWChallenge, body.PoWNonce) {
			http.Error(w, "invalid or missing proof of work", http.StatusPreconditionRequired)
			return
		}
	}

	instance := &kimov1alpha1.ChallengeInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instanceName(body.Template, body.Team, body.Player),
			Namespace: s.namespace,
			Labels: map[string]string{
				kimov1alpha1.LabelChallenge: body.Template,
				kimov1alpha1.LabelTeam:      body.Team,
			},
		},
		Spec: kimov1alpha1.ChallengeInstanceSpec{
			TemplateRef: body.Template,
			Team:        body.Team,
			Player:      body.Player,
		},
	}
	if err := s.client.Create(r.Context(), instance); err != nil {
		if errors.IsAlreadyExists(err) {
			http.Error(w, "instance already exists", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, instance)
}

func instanceName(template, team, player string) string {
	name := template + "-" + team
	if player != "" {
		name += "-" + player
	}
	return name
}

func (s *Server) handleListInstances(w http.ResponseWriter, r *http.Request) {
	var list kimov1alpha1.ChallengeInstanceList
	opts := []client.ListOption{client.InNamespace(s.namespace)}

	labels := client.MatchingLabels{}
	if team := r.URL.Query().Get("team"); team != "" {
		labels[kimov1alpha1.LabelTeam] = team
	}
	if challenge := r.URL.Query().Get("challenge"); challenge != "" {
		labels[kimov1alpha1.LabelChallenge] = challenge
	}
	if len(labels) > 0 {
		opts = append(opts, labels)
	}

	if err := s.client.List(r.Context(), &list, opts...); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	items := list.Items
	if status := r.URL.Query().Get("status"); status != "" {
		filtered := items[:0]
		for _, item := range items {
			if string(item.Status.Phase) == status {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}

	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleGetInstance(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var instance kimov1alpha1.ChallengeInstance
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: name, Namespace: s.namespace}, &instance); err != nil {
		if errors.IsNotFound(err) {
			http.Error(w, "instance not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, instance)
}

func (s *Server) handleDeleteInstance(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	instance := &kimov1alpha1.ChallengeInstance{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: s.namespace},
	}
	if err := s.client.Delete(r.Context(), instance); err != nil {
		if errors.IsNotFound(err) {
			http.Error(w, "instance not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type extendInstanceRequest struct {
	TTL string `json:"ttl"`
}

func (s *Server) handleExtendInstance(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	var body extendInstanceRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if _, err := time.ParseDuration(body.TTL); err != nil {
		http.Error(w, "invalid ttl: "+err.Error(), http.StatusBadRequest)
		return
	}

	var instance kimov1alpha1.ChallengeInstance
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: name, Namespace: s.namespace}, &instance); err != nil {
		if errors.IsNotFound(err) {
			http.Error(w, "instance not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	instance.Spec.TTLOverride = body.TTL
	if err := s.client.Update(r.Context(), &instance); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, instance)
}

func (s *Server) handlePoWChallenge(w http.ResponseWriter, r *http.Request) {
	tmplName := r.URL.Query().Get("template")
	if tmplName == "" {
		http.Error(w, "template query parameter is required", http.StatusBadRequest)
		return
	}

	var tmpl kimov1alpha1.ChallengeTemplate
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: tmplName, Namespace: s.namespace}, &tmpl); err != nil {
		if errors.IsNotFound(err) {
			http.Error(w, "template not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if tmpl.Spec.PoW == nil || !tmpl.Spec.PoW.Enabled {
		writeJSON(w, http.StatusOK, map[string]bool{"required": false})
		return
	}

	puzzle := GeneratePoWPuzzle(tmpl.Spec.PoW.Difficulty)
	s.storePoW(puzzle)
	writeJSON(w, http.StatusOK, puzzle)
}

func (s *Server) storePoW(puzzle PoWPuzzle) {
	s.powMu.Lock()
	defer s.powMu.Unlock()
	s.puzzles[puzzle.Challenge] = puzzle
}

// consumePoW verifies the nonce against the puzzle issued for challenge and,
// on success, removes it so it can't be replayed.
func (s *Server) consumePoW(challenge string, nonce uint64) bool {
	s.powMu.Lock()
	puzzle, ok := s.puzzles[challenge]
	if ok {
		delete(s.puzzles, challenge)
	}
	s.powMu.Unlock()

	if !ok || time.Now().After(puzzle.ExpiresAt) {
		return false
	}
	return VerifyPoW(puzzle.Challenge, nonce, puzzle.Difficulty)
}
