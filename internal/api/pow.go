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
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

type PoWPuzzle struct {
	Challenge  string    `json:"challenge"`
	Difficulty int       `json:"difficulty"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

func GeneratePoWPuzzle(difficulty int) PoWPuzzle {
	b := make([]byte, 32)
	rand.Read(b)
	return PoWPuzzle{
		Challenge:  hex.EncodeToString(b),
		Difficulty: difficulty,
		ExpiresAt:  time.Now().Add(5 * time.Minute),
	}
}

func VerifyPoW(challenge string, nonce uint64, difficulty int) bool {
	data := fmt.Sprintf("%s:%d", challenge, nonce)
	hash := sha256.Sum256([]byte(data))
	return countLeadingZeroBits(hash[:]) >= difficulty
}

func countLeadingZeroBits(hash []byte) int {
	count := 0
	for _, b := range hash {
		if b == 0 {
			count += 8
			continue
		}
		for i := 7; i >= 0; i-- {
			if b&(1<<uint(i)) == 0 {
				count++
			} else {
				return count
			}
		}
	}
	return count
}

// SolvePoW is a helper for testing — brute-force solver.
func SolvePoW(challenge string, difficulty int) (uint64, bool) {
	for nonce := uint64(0); nonce < 1<<30; nonce++ {
		if VerifyPoW(challenge, nonce, difficulty) {
			return nonce, true
		}
	}
	return 0, false
}
