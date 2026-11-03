package opaquetoken

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

func TestTokens(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		token := New()
		decoded, err := base64.RawURLEncoding.Strict().DecodeString(strings.TrimPrefix(token, prefix))
		if err != nil || len(decoded) != 32 || !Valid(token) || seen[token] {
			t.Fatal("invalid or duplicate token")
		}
		seen[token] = true
		if Hash(token) != fmt.Sprintf("%x", sha256.Sum256([]byte(token))) {
			t.Fatal("incorrect hash")
		}
	}
}

func TestInvalidFormats(t *testing.T) {
	token := New()
	for _, raw := range []string{"", "old.jwt.token", strings.Repeat("a", 4096), token + "=", token[1:], "hs2_" + token[4:], token[:46] + "!", "hs1_" + strings.Repeat("A", 42) + "B"} {
		if Valid(raw) {
			t.Fatal("invalid token accepted")
		}
	}
}
