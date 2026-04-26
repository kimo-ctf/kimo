package pow_test

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/chxmxii/kimo/internal/pow"
)

// ---- helpers ----------------------------------------------------------------

// solve finds a nonce for the given prefix and difficulty (for testing only).
func solve(prefix string, difficulty int) string {
	for i := 0; ; i++ {
		nonce := fmt.Sprintf("%d", i)
		input := prefix + ":" + nonce
		hash := sha256.Sum256([]byte(input))
		if leadingZeroBits(hash[:]) >= difficulty {
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

func TestIssue(t *testing.T) {
	mgr := pow.New(4, time.Minute)
	ch, err := mgr.Issue()
	if err != nil {
		t.Fatalf("Issue() error: %v", err)
	}
	if ch.ID == "" {
		t.Error("challenge ID must not be empty")
	}
	if ch.Prefix == "" {
		t.Error("challenge Prefix must not be empty")
	}
	if ch.Difficulty != 4 {
		t.Errorf("Difficulty: got %d, want 4", ch.Difficulty)
	}
	if ch.ExpiresAt.Before(time.Now()) {
		t.Error("challenge must not already be expired")
	}
}

func TestVerify_Valid(t *testing.T) {
	const difficulty = 4
	mgr := pow.New(difficulty, time.Minute)

	ch, err := mgr.Issue()
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	nonce := solve(ch.Prefix, difficulty)
	if err := mgr.Verify(ch.ID, nonce); err != nil {
		t.Fatalf("Verify: unexpected error: %v", err)
	}
}

func TestVerify_InvalidSolution(t *testing.T) {
	mgr := pow.New(20, time.Minute)
	ch, _ := mgr.Issue()

	// A nonce of "wrong" is astronomically unlikely to solve difficulty=20.
	err := mgr.Verify(ch.ID, "wrong")
	if err != pow.ErrInvalidSolution {
		t.Errorf("expected ErrInvalidSolution, got %v", err)
	}
}

func TestVerify_NotFound(t *testing.T) {
	mgr := pow.New(4, time.Minute)
	err := mgr.Verify("nonexistent-id", "any-nonce")
	if err != pow.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestVerify_Expired(t *testing.T) {
	// Use a very short TTL so the challenge expires before we verify.
	mgr := pow.New(4, 10*time.Millisecond)
	ch, _ := mgr.Issue()
	time.Sleep(50 * time.Millisecond)

	err := mgr.Verify(ch.ID, "any-nonce")
	if err != pow.ErrExpired {
		t.Errorf("expected ErrExpired, got %v", err)
	}
}

func TestVerify_AlreadyConsumed(t *testing.T) {
	const difficulty = 4
	mgr := pow.New(difficulty, time.Minute)
	ch, _ := mgr.Issue()
	nonce := solve(ch.Prefix, difficulty)

	// First verify should succeed.
	if err := mgr.Verify(ch.ID, nonce); err != nil {
		t.Fatalf("first Verify: %v", err)
	}

	// Second verify with the same (already consumed) challenge must fail.
	err := mgr.Verify(ch.ID, nonce)
	if err != pow.ErrAlreadyConsumed {
		t.Errorf("expected ErrAlreadyConsumed, got %v", err)
	}
}

func TestVerify_ReplayRejected(t *testing.T) {
	// Confirm that replaying a valid solution does not succeed.
	const difficulty = 4
	mgr := pow.New(difficulty, time.Minute)
	ch, _ := mgr.Issue()
	nonce := solve(ch.Prefix, difficulty)

	_ = mgr.Verify(ch.ID, nonce)

	// Attempt replay.
	if err := mgr.Verify(ch.ID, nonce); err == nil {
		t.Error("replay must be rejected")
	}
}

func TestIssue_UniqueIDs(t *testing.T) {
	mgr := pow.New(4, time.Minute)
	ids := make(map[string]struct{})
	for i := 0; i < 100; i++ {
		ch, err := mgr.Issue()
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		if _, dup := ids[ch.ID]; dup {
			t.Fatalf("duplicate challenge ID: %s", ch.ID)
		}
		ids[ch.ID] = struct{}{}
	}
}
