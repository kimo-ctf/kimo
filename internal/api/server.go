// Package api implements the kimo HTTP REST API.
//
// Endpoints:
//
//	GET  /api/v1/pow/challenge             issue a new PoW challenge
//	POST /api/v1/instances                 create an instance (requires PoW)
//	GET  /api/v1/instances                 list all instances
//	GET  /api/v1/instances/{id}            get a single instance
//	DELETE /api/v1/instances/{id}          delete an instance (requires PoW)
package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/chxmxii/kimo/internal/instance"
	"github.com/chxmxii/kimo/internal/pow"
	"github.com/chxmxii/kimo/internal/vm"
)

// Server is the kimo HTTP API server.
type Server struct {
	mgr    *instance.Manager
	mux    *http.ServeMux
	logger *log.Logger
}

// New creates a new API Server and registers all routes.
func New(mgr *instance.Manager, logger *log.Logger) *Server {
	s := &Server{
		mgr:    mgr,
		mux:    http.NewServeMux(),
		logger: logger,
	}
	s.routes()
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("/api/v1/pow/challenge", s.handlePoWChallenge)
	s.mux.HandleFunc("/api/v1/instances", s.handleInstances)
	s.mux.HandleFunc("/api/v1/instances/", s.handleInstance)
	s.mux.HandleFunc("/healthz", s.handleHealth)
}

// ---- handlers ---------------------------------------------------------------

// handlePoWChallenge issues a new PoW challenge.
//
//	GET /api/v1/pow/challenge
func (s *Server) handlePoWChallenge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ch, err := s.mgr.IssuePoW()
	if err != nil {
		s.internalError(w, "issuing PoW challenge", err)
		return
	}
	writeJSON(w, http.StatusOK, ch)
}

// handleInstances dispatches GET (list) and POST (create).
//
//	GET  /api/v1/instances
//	POST /api/v1/instances
func (s *Server) handleInstances(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listInstances(w, r)
	case http.MethodPost:
		s.createInstance(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) listInstances(w http.ResponseWriter, r *http.Request) {
	insts := s.mgr.List(r.Context())
	writeJSON(w, http.StatusOK, map[string]interface{}{"instances": insts})
}

func (s *Server) createInstance(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TeamID         string  `json:"team_id"`
		TeamToken      string  `json:"team_token"`
		CTFChallengeID string  `json:"ctf_challenge_id"`
		PoWChallengeID string  `json:"pow_challenge_id"`
		PoWNonce       string  `json:"pow_nonce"`
		VMSpec         vm.Spec `json:"vm_spec"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.TeamID == "" || req.CTFChallengeID == "" || req.PoWChallengeID == "" || req.PoWNonce == "" {
		writeError(w, http.StatusBadRequest, "team_id, ctf_challenge_id, pow_challenge_id, and pow_nonce are required")
		return
	}

	inst, err := s.mgr.Create(r.Context(), instance.CreateRequest{
		TeamID:         req.TeamID,
		TeamToken:      req.TeamToken,
		CTFChallengeID: req.CTFChallengeID,
		PoWChallengeID: req.PoWChallengeID,
		PoWNonce:       req.PoWNonce,
		VMSpec:         req.VMSpec,
	})
	if err != nil {
		s.handleManagerError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, inst)
}

// handleInstance dispatches GET and DELETE for a specific instance.
//
//	GET    /api/v1/instances/{id}
//	DELETE /api/v1/instances/{id}
func (s *Server) handleInstance(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/instances/")
	if id == "" {
		writeError(w, http.StatusBadRequest, "instance id is required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		inst, err := s.mgr.Get(r.Context(), id)
		if err != nil {
			s.handleManagerError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, inst)

	case http.MethodDelete:
		var req struct {
			PoWChallengeID string `json:"pow_challenge_id"`
			PoWNonce       string `json:"pow_nonce"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}
		if req.PoWChallengeID == "" || req.PoWNonce == "" {
			writeError(w, http.StatusBadRequest, "pow_challenge_id and pow_nonce are required")
			return
		}
		if err := s.mgr.Delete(r.Context(), id, req.PoWChallengeID, req.PoWNonce); err != nil {
			s.handleManagerError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---- error helpers ----------------------------------------------------------

func (s *Server) handleManagerError(w http.ResponseWriter, err error) {
	switch {
	case containsAny(err, "PoW verification failed"):
		writeError(w, http.StatusBadRequest, err.Error())
	case containsAny(err, "not found"):
		writeError(w, http.StatusNotFound, err.Error())
	case containsAny(err, "team validation failed", "unauthorized"):
		writeError(w, http.StatusUnauthorized, err.Error())
	default:
		s.internalError(w, "manager error", err)
	}
}

func (s *Server) internalError(w http.ResponseWriter, msg string, err error) {
	s.logger.Printf("ERROR %s: %v", msg, err)
	writeError(w, http.StatusInternalServerError, "internal server error")
}

// ---- JSON helpers -----------------------------------------------------------

type errorResponse struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// Headers already sent; log only.
		_ = err
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

func containsAny(err error, subs ...string) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, s := range subs {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// powErrors bundles all PoW sentinel errors for HTTP mapping.
var powErrors = []error{
	pow.ErrNotFound,
	pow.ErrExpired,
	pow.ErrAlreadyConsumed,
	pow.ErrInvalidSolution,
}

// ensure the compiler checks the import is used.
var _ = fmt.Sprintf
var _ = powErrors
