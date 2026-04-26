package api_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chxmxii/kimo/internal/api"
	"github.com/chxmxii/kimo/internal/instance"
	"github.com/chxmxii/kimo/internal/platform"
	"github.com/chxmxii/kimo/internal/pow"
	"github.com/chxmxii/kimo/internal/vm"
)

// ---- fakes ------------------------------------------------------------------

type fakePlatform struct{}

func (f *fakePlatform) Name() string                     { return "fake" }
func (f *fakePlatform) Validate(_ context.Context) error { return nil }
func (f *fakePlatform) GetChallenge(_ context.Context, id string) (*platform.Challenge, error) {
	return &platform.Challenge{ID: id, Name: "test"}, nil
}
func (f *fakePlatform) ValidateAccess(_ context.Context, _, _ string) error { return nil }

type fakeVM struct{}

func (f *fakeVM) Name() string                     { return "fake" }
func (f *fakeVM) Validate(_ context.Context) error { return nil }
func (f *fakeVM) CreateVM(_ context.Context, spec vm.Spec) (*vm.Instance, error) {
	return &vm.Instance{
		ID:        "vm-test-123",
		Status:    vm.StatusRunning,
		CreatedAt: time.Now(),
		Spec:      spec,
	}, nil
}
func (f *fakeVM) DeleteVM(_ context.Context, _ string) error { return nil }
func (f *fakeVM) GetVM(_ context.Context, id string) (*vm.Instance, error) {
	return &vm.Instance{ID: id, Status: vm.StatusRunning}, nil
}
func (f *fakeVM) ListVMs(_ context.Context) ([]*vm.Instance, error) {
	return []*vm.Instance{}, nil
}

// ---- test helpers -----------------------------------------------------------

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	powMgr := pow.New(1, time.Minute) // difficulty=1 for fast tests
	mgr := instance.New(&fakePlatform{}, &fakeVM{}, powMgr)
	logger := log.New(io.Discard, "", 0)
	srv := api.New(mgr, logger)
	return httptest.NewServer(srv)
}

// solvePow iterates nonces until SHA-256(prefix+":"+nonce) satisfies difficulty.
func solvePow(prefix string, difficulty int) string {
	for i := 0; ; i++ {
		nonce := fmt.Sprintf("%d", i)
		input := prefix + ":" + nonce
		h := sha256.Sum256([]byte(input))
		if leadingZeroBits(h[:]) >= difficulty {
			return nonce
		}
	}
}

func leadingZeroBits(b []byte) int {
	count := 0
	for _, v := range b {
		if v == 0 {
			count += 8
			continue
		}
		for mask := byte(0x80); mask != 0; mask >>= 1 {
			if v&mask != 0 {
				return count
			}
			count++
		}
		return count
	}
	return count
}

// ---- tests ------------------------------------------------------------------

func TestHealth(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /healthz: status %d, want 200", resp.StatusCode)
	}
}

func TestGetPoWChallenge(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/pow/challenge")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status %d, want 200", resp.StatusCode)
	}

	var ch pow.Challenge
	if err := json.NewDecoder(resp.Body).Decode(&ch); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if ch.ID == "" {
		t.Error("challenge ID must not be empty")
	}
	if ch.Prefix == "" {
		t.Error("challenge Prefix must not be empty")
	}
}

func TestListInstances_Empty(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/instances")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status %d, want 200", resp.StatusCode)
	}
}

func TestCreateInstance_MissingPoW(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	body := `{"team_id":"t1","ctf_challenge_id":"c1","pow_challenge_id":"bad","pow_nonce":"0"}`
	resp, err := http.Post(ts.URL+"/api/v1/instances", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Invalid PoW → 400
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status %d, want 400", resp.StatusCode)
	}
}

func TestCreateInstance_ValidPoW(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	// Get a challenge.
	chResp, err := http.Get(ts.URL + "/api/v1/pow/challenge")
	if err != nil {
		t.Fatal(err)
	}
	var ch pow.Challenge
	if err := json.NewDecoder(chResp.Body).Decode(&ch); err != nil {
		chResp.Body.Close()
		t.Fatalf("decoding challenge: %v", err)
	}
	chResp.Body.Close()

	nonce := solvePow(ch.Prefix, ch.Difficulty)

	reqBody, _ := json.Marshal(map[string]interface{}{
		"team_id":          "t1",
		"team_token":       "tok",
		"ctf_challenge_id": "c1",
		"pow_challenge_id": ch.ID,
		"pow_nonce":        nonce,
		"vm_spec":          map[string]interface{}{"memory_mb": 256, "cpus": 1},
	})

	resp, err := http.Post(ts.URL+"/api/v1/instances", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d, want 201; body: %s", resp.StatusCode, body)
	}

	var inst instance.Instance
	if err := json.NewDecoder(resp.Body).Decode(&inst); err != nil {
		t.Fatalf("decoding instance: %v", err)
	}
	if inst.ID == "" {
		t.Error("instance ID must not be empty")
	}
}

func TestPoWChallenge_MethodNotAllowed(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/pow/challenge", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status %d, want 405", resp.StatusCode)
	}
}

