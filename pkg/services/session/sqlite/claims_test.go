package sqlite

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	sessions "github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/session/internal/sessioncookie"
)

func TestSessionStoresRejectInvalidClaims(t *testing.T) {
	for _, provider := range []string{"cookie", "sqlite"} {
		t.Run(provider, func(t *testing.T) {
			var store sessions.SessionStore
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
			if _, err := sessioncookie.Parse(cookie.Value, key); err != nil {
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
