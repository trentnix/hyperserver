// Package opaquetoken creates versioned, URL-safe bearer tokens.
package opaquetoken

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
)

const prefix = "hs1_"

// New returns a token with 256 bits of randomness and no embedded identity.
func New() string {
	var value [32]byte
	rand.Read(value[:])
	return prefix + base64.RawURLEncoding.EncodeToString(value[:])
}

// Valid rejects unsupported formats and noncanonical encodings before storage access.
func Valid(token string) bool {
	if len(token) != len(prefix)+43 || !strings.HasPrefix(token, prefix) {
		return false
	}
	value, err := base64.RawURLEncoding.Strict().DecodeString(token[len(prefix):])
	return err == nil && len(value) == 32
}

// Hash returns the lowercase SHA-256 digest. Never persist the bearer token.
func Hash(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}
