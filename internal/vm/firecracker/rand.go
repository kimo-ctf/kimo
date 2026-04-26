package firecracker

import "crypto/rand"

// cryptoRead is a thin wrapper so tests can intercept random bytes.
var cryptoRead = func(b []byte) (int, error) { return rand.Read(b) }
