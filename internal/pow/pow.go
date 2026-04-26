// Package pow implements a hashcash-style proof-of-work scheme used to rate-
// limit VM instance creation.
//
// # Protocol
//
//  1. The server calls Manager.Issue() to obtain a Challenge.  The challenge
//     contains an opaque random Prefix and the required Difficulty (number of
//     leading zero bits).
//
//  2. The challenge is returned to the client together with the response to
//     "GET /api/v1/pow/challenge".
//
//  3. The client iterates nonce values (any printable string) until it finds
//     one where:
//
//     SHA-256( prefix + ":" + nonce )
//
//     has at least Difficulty leading zero bits.
//
//  4. The client includes the ChallengeID and the winning Nonce in the body
//     of the instance-creation request.
//
//  5. The server calls Manager.Verify(id, nonce).  A challenge may only be
//     verified once; subsequent calls return ErrAlreadyConsumed.
package pow

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Sentinel errors returned by Manager.Verify.
var (
	// ErrNotFound is returned when the challenge ID is unknown.
	ErrNotFound = errors.New("pow: challenge not found")
	// ErrExpired is returned when the challenge TTL has elapsed.
	ErrExpired = errors.New("pow: challenge expired")
	// ErrAlreadyConsumed is returned when the challenge was already used.
	ErrAlreadyConsumed = errors.New("pow: challenge already consumed")
	// ErrInvalidSolution is returned when the nonce does not satisfy the
	// difficulty requirement.
	ErrInvalidSolution = errors.New("pow: invalid solution")
)

// Challenge is the value issued to a client.
type Challenge struct {
	// ID is an opaque server-generated identifier used to look up the
	// challenge on verification.
	ID string `json:"id"`
	// Prefix is the random hex-encoded bytes the client must incorporate into
	// its hash input.
	Prefix string `json:"prefix"`
	// Difficulty is the number of leading zero bits required.
	Difficulty int `json:"difficulty"`
	// ExpiresAt is the deadline after which the challenge is no longer valid.
	ExpiresAt time.Time `json:"expires_at"`
}

// entry is the server-side record for an issued challenge.
type entry struct {
	ch       Challenge
	consumed bool
}

// Manager issues and verifies proof-of-work challenges.
// It is safe for concurrent use.
type Manager struct {
	difficulty int
	ttl        time.Duration

	mu         sync.Mutex
	challenges map[string]*entry
}

// New creates a Manager with the given difficulty and TTL.
//
//   - difficulty: number of leading zero bits required in a valid solution
//     (recommended 20 ≈ 1M SHA-256 iterations on average).
//   - ttl: how long a challenge remains valid before it expires.
func New(difficulty int, ttl time.Duration) *Manager {
	m := &Manager{
		difficulty: difficulty,
		ttl:        ttl,
		challenges: make(map[string]*entry),
	}
	go m.gcLoop()
	return m
}

// Issue creates a new challenge, stores it, and returns it to the caller.
func (m *Manager) Issue() (*Challenge, error) {
	id, err := randomHex(16)
	if err != nil {
		return nil, fmt.Errorf("pow: generating challenge id: %w", err)
	}
	prefix, err := randomHex(16)
	if err != nil {
		return nil, fmt.Errorf("pow: generating prefix: %w", err)
	}

	ch := Challenge{
		ID:         id,
		Prefix:     prefix,
		Difficulty: m.difficulty,
		ExpiresAt:  time.Now().Add(m.ttl),
	}

	m.mu.Lock()
	m.challenges[id] = &entry{ch: ch}
	m.mu.Unlock()

	return &ch, nil
}

// Verify checks that nonce is a valid solution for the challenge identified by
// id.  On success it marks the challenge as consumed so it cannot be reused.
//
// Errors: ErrNotFound, ErrExpired, ErrAlreadyConsumed, ErrInvalidSolution.
func (m *Manager) Verify(id, nonce string) error {
	m.mu.Lock()
	e, ok := m.challenges[id]
	m.mu.Unlock()

	if !ok {
		return ErrNotFound
	}
	if time.Now().After(e.ch.ExpiresAt) {
		return ErrExpired
	}
	if e.consumed {
		return ErrAlreadyConsumed
	}
	if !checkSolution(e.ch.Prefix, nonce, e.ch.Difficulty) {
		return ErrInvalidSolution
	}

	m.mu.Lock()
	e.consumed = true
	m.mu.Unlock()

	return nil
}

// ---- solution checking ------------------------------------------------------

// checkSolution returns true when SHA-256(prefix + ":" + nonce) has at least
// difficulty leading zero bits.
func checkSolution(prefix, nonce string, difficulty int) bool {
	input := prefix + ":" + nonce
	hash := sha256.Sum256([]byte(input))
	return leadingZeroBits(hash[:]) >= difficulty
}

// leadingZeroBits counts the number of leading zero bits in b.
func leadingZeroBits(b []byte) int {
	count := 0
	for _, v := range b {
		if v == 0 {
			count += 8
			continue
		}
		// Count leading zero bits in this byte.
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

// ---- garbage collection -----------------------------------------------------

// gcLoop periodically removes old challenges to prevent unbounded memory growth.
func (m *Manager) gcLoop() {
	ticker := time.NewTicker(m.ttl)
	defer ticker.Stop()
	for range ticker.C {
		m.gc()
	}
}

// gc removes challenges that have been expired for at least gcGrace(ttl).
// We keep recently-expired challenges in memory so that Verify can return
// ErrExpired (rather than ErrNotFound) for a reasonable window after expiry.
func (m *Manager) gc() {
	grace := gcGrace(m.ttl)
	cutoff := time.Now().Add(-grace)

	m.mu.Lock()
	defer m.mu.Unlock()
	for id, e := range m.challenges {
		if e.ch.ExpiresAt.Before(cutoff) {
			delete(m.challenges, id)
		}
	}
}

// gcGrace returns the extra retention period after a challenge expires.
// It is at least 30 seconds regardless of ttl so that even very short TTLs
// in tests allow Verify to distinguish ErrExpired from ErrNotFound.
func gcGrace(ttl time.Duration) time.Duration {
	grace := ttl * 3
	if grace < 30*time.Second {
		grace = 30 * time.Second
	}
	return grace
}

// ---- helpers ----------------------------------------------------------------

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
