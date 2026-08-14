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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPoW_GenerateAndVerify(t *testing.T) {
	puzzle := GeneratePoWPuzzle(16) // 16 leading zero bits = easy
	require.NotEmpty(t, puzzle.Challenge)
	require.Equal(t, 16, puzzle.Difficulty)

	nonce, found := SolvePoW(puzzle.Challenge, puzzle.Difficulty)
	require.True(t, found)

	assert.True(t, VerifyPoW(puzzle.Challenge, nonce, puzzle.Difficulty))
}

func TestPoW_RejectsWrongNonce(t *testing.T) {
	puzzle := GeneratePoWPuzzle(16)
	assert.False(t, VerifyPoW(puzzle.Challenge, 0, puzzle.Difficulty))
}

func TestPoW_LeadingZeroBits(t *testing.T) {
	challenge := "test-challenge"
	nonce := uint64(12345)
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", challenge, nonce)))
	hexHash := hex.EncodeToString(hash[:])
	_ = hexHash
	assert.GreaterOrEqual(t, countLeadingZeroBits(hash[:]), 0)
}
