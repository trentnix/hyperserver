package session

import (
	"net/http"
	"net/http/httptest"
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
	} {
		t.Run(name, func(t *testing.T) {
			expires := time.Now().Add(time.Hour).Truncate(time.Second)
			claims := jwt.MapClaims{"id": "session-id", "purpose": "session", "exp": expires.Unix()}
			key := []byte("test-session-key")
			var signingKey any = key
			var method jwt.SigningMethod = jwt.SigningMethodHS256
			switch name {
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

			got, err := parseSessionJWT(raw, key)
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

func TestSessionStoresRejectInvalidClaims(t *testing.T) {
	for _, provider := range []string{"cookie", "sqlite"} {
		t.Run(provider, func(t *testing.T) {
			var store SessionStore
			if provider == "cookie" {
				store = setupCookieStore(t)
			} else {
				store = setupSQLiteStore(t)
			}
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			sess, err := store.New(r, "jwt-validation")
			if err != nil {
				t.Fatal(err)
			}
			sess.Data["userID"] = "test-user"
			w := httptest.NewRecorder()
			if err := store.Save(w, r, sess); err != nil {
				t.Fatal(err)
			}
			cookie := w.Result().Cookies()[0]
			key := []byte("secretKey")
			if _, err := parseSessionJWT(cookie.Value, key); err != nil {
				t.Fatalf("store issued an invalid session token: %v", err)
			}

			for _, failure := range []string{"missing expiry", "wrong algorithm", "wrong purpose"} {
				t.Run(failure, func(t *testing.T) {
					claims := jwt.MapClaims{}
					if _, err := jwt.ParseWithClaims(cookie.Value, claims, func(*jwt.Token) (any, error) { return key, nil }); err != nil {
						t.Fatal(err)
					}
					method := jwt.SigningMethodHS256
					switch failure {
					case "missing expiry":
						delete(claims, "exp")
					case "wrong algorithm":
						method = jwt.SigningMethodHS512
					case "wrong purpose":
						claims["purpose"] = "auth-reset"
					}
					raw, err := jwt.NewWithClaims(method, claims).SignedString(key)
					if err != nil {
						t.Fatal(err)
					}
					request := httptest.NewRequest(http.MethodGet, "/", nil)
					request.AddCookie(&http.Cookie{Name: cookie.Name, Value: raw})
					got, err := store.Get(request, cookie.Name)
					if err == nil || (got != nil && (!got.IsNew || len(got.Data) != 0)) {
						t.Fatal("invalid cookie restored a session")
					}
				})
			}
		})
	}
}

func TestSessionClaimsWithoutExpiry(t *testing.T) {
	if got := (&SessionClaims{}).ExpiresAtTime(); !got.IsZero() {
		t.Fatalf("missing expiry = %v, want zero time", got)
	}
}
