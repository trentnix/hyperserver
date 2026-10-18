package sessioncookie

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestParseSessionJWT(t *testing.T) {
	for _, name := range []string{
		"valid", "missing expiry", "null expiry", "zero expiry", "expired", "invalid expiry",
		"missing ID", "missing purpose", "reset purpose", "verification purpose",
		"HS384", "HS512", "unsigned", "wrong key", "empty key",
		"future issued at", "future not before", "malformed", "empty token",
		"missing name", "wrong name", "empty expected name",
	} {
		t.Run(name, func(t *testing.T) {
			expires := time.Now().Add(time.Hour).Truncate(time.Second)
			claims := jwt.MapClaims{"id": "session-id", "name": "account", "purpose": "session", "exp": expires.Unix()}
			expectedName := "account"
			key := []byte("test-session-key")
			var signingKey any = key
			var method jwt.SigningMethod = jwt.SigningMethodHS256
			switch name {
			case "missing name":
				delete(claims, "name")
			case "wrong name":
				claims["name"] = "other"
			case "empty expected name":
				expectedName = ""
			case "missing expiry":
				delete(claims, "exp")
			case "null expiry":
				claims["exp"] = nil
			case "zero expiry":
				claims["exp"] = 0
			case "expired":
				claims["exp"] = time.Now().Add(-time.Hour).Unix()
			case "invalid expiry":
				claims["exp"] = "tomorrow"
			case "missing ID":
				delete(claims, "id")
			case "missing purpose":
				delete(claims, "purpose")
			case "reset purpose":
				claims["purpose"] = "auth-reset"
			case "verification purpose":
				claims["purpose"] = "auth-verification"
			case "HS384":
				method = jwt.SigningMethodHS384
			case "HS512":
				method = jwt.SigningMethodHS512
			case "unsigned":
				method, signingKey = jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType
			case "future issued at":
				claims["iat"] = time.Now().Add(time.Hour).Unix()
			case "future not before":
				claims["nbf"] = time.Now().Add(time.Hour).Unix()
			}
			raw, err := jwt.NewWithClaims(method, claims).SignedString(signingKey)
			if err != nil {
				t.Fatal(err)
			}
			switch name {
			case "wrong key":
				key = []byte("another-key")
			case "empty key":
				key = nil
			case "malformed":
				raw = "not-a-token"
			case "empty token":
				raw = ""
			}

			got, err := Parse(raw, key, expectedName)
			if name != "valid" {
				if err == nil || got != nil {
					t.Fatal("invalid session token accepted")
				}
				return
			}
			if err != nil || got == nil {
				t.Fatalf("valid session token rejected: %v", err)
			}
			if got.ID != "session-id" || !got.ExpiresAtTime().Equal(expires) {
				t.Fatal("session claims changed during parsing")
			}
		})
	}
}

func TestSessionClaimsWithoutExpiry(t *testing.T) {
	if got := (&Claims{}).ExpiresAtTime(); !got.IsZero() {
		t.Fatalf("missing expiry = %v, want zero time", got)
	}
}
