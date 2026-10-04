package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/trentnix/hyperserver/pkg/services/middleware"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestHTTPSQLiteSessionRevocation(t *testing.T) {
	for _, scenario := range []string{"logout", "stored expiry", "failed logout"} {
		t.Run(scenario, func(t *testing.T) {
			runHTTPScenario(t, func(h *httpHarness) {
				account := h.seedUser(t, "revoke@example.invalid")
				w := h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {account.Email}, "password": {"TestPassword1!"}}, true)
				if w.Header().Get("HX-Redirect") != "/" {
					t.Fatal("login failed", w.Body.String())
				}
				base, err := url.Parse(h.baseURL)
				if err != nil {
					t.Fatal(err)
				}
				var captured *http.Cookie
				for _, cookie := range h.cookies.Cookies(base) {
					if cookie.Name == "auth-user-session" {
						captured = cookie
					}
				}
				if captured == nil {
					t.Fatal("missing authentication cookie")
				}
				claims := &session.SessionClaims{}
				if _, err := jwt.ParseWithClaims(captured.Value, claims, func(*jwt.Token) (any, error) { return []byte(h.app.Config.HTTP.Session.JwtKey), nil }); err != nil {
					t.Fatal(err)
				}
				called := false
				h.app.Web.Handle("GET /revocation-protected", middleware.RequireAuthentication(h.app.Database, h.app.ContentManager)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					called = true
					u := user.GetUserFromContext(r.Context())
					if u == nil || u.ID != account.ID {
						t.Fatal("protected request lost the account")
					}
					w.WriteHeader(http.StatusNoContent)
				})))
				w = h.request(http.MethodGet, "/revocation-protected", nil, true)
				if w.Code != http.StatusNoContent || !called {
					t.Fatal("active session denied access")
				}

				failLogout := scenario == "failed logout"
				logout, _ := h.app.Web.Handler(httptest.NewRequest(http.MethodPost, "/auth/logout", nil))
				h.app.Web.Handle("POST /logout-observed", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					logout.ServeHTTP(w, r)
					u, err := user.GetAuthenticatedUser(r, h.app.Database)
					if err != nil {
						t.Fatal(err)
					}
					s, err := session.Get(r, "auth-user-session")
					if err != nil {
						t.Fatal(err)
					}
					if failLogout {
						if u == nil || u.ID != account.ID || s.ID != claims.ID {
							t.Fatal("failed logout cleared request-local identity")
						}
					} else if u != nil || user.GetUserFromContext(r.Context()) != nil || !s.IsNew || len(s.Data) != 0 || s.ID == claims.ID {
						t.Fatal("successful logout retained request-local identity")
					}
				}))
				if scenario == "stored expiry" {
					if _, err := h.app.Database.Exec(`UPDATE session SET expires_at = ? WHERE id = ?`, time.Now().Add(-time.Minute), claims.ID); err != nil {
						t.Fatal(err)
					}
				} else {
					if failLogout {
						if _, err := h.app.Database.Exec(`CREATE TRIGGER reject_logout BEFORE DELETE ON session BEGIN SELECT RAISE(ABORT, 'logout storage failure'); END`); err != nil {
							t.Fatal(err)
						}
						w = h.request(http.MethodPost, "/logout-observed", nil, true)
						if w.Code != http.StatusInternalServerError || w.Body.String() != "There was an error trying to log out\n" || w.Header().Get("HX-Redirect") != "" || len(w.Result().Cookies()) != 0 {
							t.Fatalf("failed logout response = %d %s", w.Code, w.Body.String())
						}
						called = false
						if w = h.request(http.MethodGet, "/revocation-protected", nil, true); w.Code != http.StatusNoContent || !called {
							t.Fatal("failed logout revoked the session")
						}
						if _, err := h.app.Database.Exec(`DROP TRIGGER reject_logout`); err != nil {
							t.Fatal(err)
						}
						failLogout = false
					}
					w = h.request(http.MethodPost, "/logout-observed", nil, true)
					if w.Code != http.StatusOK || w.Header().Get("HX-Redirect") != "/" {
						t.Fatalf("logout failed: %d %s", w.Code, w.Body.String())
					}
					var count int
					if err := h.app.Database.Get(&count, `SELECT count(*) FROM session WHERE id = ?`, claims.ID); err != nil || count != 0 {
						t.Fatalf("logout left its stored session: %d, %v", count, err)
					}
				}

				// Replay the original signed cookie, ignoring any deletion sent to the browser.
				captured.Path = "/"
				h.cookies.SetCookies(base, []*http.Cookie{captured})
				called = false
				w = h.request(http.MethodGet, "/revocation-protected", nil, true)
				if called || w.Code != http.StatusOK || w.Header().Get("HX-Redirect") != "/login" {
					t.Fatalf("replayed session granted access: called=%v, status=%d, redirect=%q", called, w.Code, w.Header().Get("HX-Redirect"))
				}
				h.assertUserUnchanged(t, account)
			}, useSQLiteSessions)
		})
	}
}
