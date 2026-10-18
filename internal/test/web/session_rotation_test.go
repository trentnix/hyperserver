package web_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/trentnix/hyperserver/auth/identity"
	authmiddleware "github.com/trentnix/hyperserver/auth/middleware"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func (h *httpHarness) establishSession(t testing.TB, u *user.User) {
	t.Helper()
	r := session.AddSessionManagerToRequestContext(httptest.NewRequest(http.MethodGet, "/", nil), h.sessions)
	w := httptest.NewRecorder()
	if err := identity.SetAuthenticatedUser(w, r, u); err != nil {
		t.Fatal(err)
	}
	base, _ := url.Parse(h.baseURL)
	h.cookies.SetCookies(base, w.Result().Cookies())
}

func (h *httpHarness) authenticationCookie(t *testing.T) *http.Cookie {
	t.Helper()
	base, _ := url.Parse(h.baseURL)
	for _, cookie := range h.cookies.Cookies(base) {
		if cookie.Name == "auth-user-session" {
			cookie.Path = "/"
			return cookie
		}
	}
	t.Fatal("missing authentication cookie")
	return nil
}

func TestHTTPAuthenticationRotation(t *testing.T) {
	for _, operation := range []string{"login", "verification", "failed login rotation"} {
		t.Run(operation, func(t *testing.T) {
			runSiteScenario(t, func(h *httpHarness) {
				u := h.seedUser(t, "rotate@example.invalid")
				if operation == "verification" {
					u.Verified, u.VerificationRequired = false, true
					if err := h.accounts.Update(context.Background(), u); err != nil {
						t.Fatal(err)
					}
				}
				if operation == "verification" {
					h.establishSession(t, u)
				} else {
					// Begin with a persisted anonymous session to exercise fixation prevention.
					r := session.AddSessionManagerToRequestContext(httptest.NewRequest(http.MethodGet, "/", nil), h.sessions)
					s, err := session.New(r, "auth-user-session")
					if err != nil {
						t.Fatal(err)
					}
					w := httptest.NewRecorder()
					if err := s.Save(w, r); err != nil {
						t.Fatal(err)
					}
					base, _ := url.Parse(h.baseURL)
					h.cookies.SetCookies(base, w.Result().Cookies())
				}
				old := h.authenticationCookie(t)
				if operation == "failed login rotation" {
					if _, err := h.db.Exec(`CREATE TRIGGER reject_rotation BEFORE INSERT ON session BEGIN SELECT RAISE(ABORT, 'private failure'); END`); err != nil {
						t.Fatal(err)
					}
				}
				var w *httptest.ResponseRecorder
				if operation == "verification" {
					token, err := user.NewAuthVerificationToken(u, []byte(h.app.Config.Auth.JwtKey), time.Hour)
					if err != nil {
						t.Fatal(err)
					}
					if err := h.accounts.InsertToken(t.Context(), token.Metadata()); err != nil {
						t.Fatal(err)
					}
					w = h.request(http.MethodPost, "/auth/verify", url.Values{"token": {token.Token}}, true)
				} else {
					w = h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {u.Email}, "password": {"TestPassword1!"}}, true)
				}
				failed := operation == "failed login rotation"
				if failed {
					if w.Code != http.StatusInternalServerError || len(w.Result().Cookies()) != 0 || w.Header().Get("HX-Redirect") != "" || strings.Contains(w.Body.String(), "private failure") {
						t.Fatalf("failed rotation response: %d %s", w.Code, w.Body.String())
					}
				} else if w.Code != http.StatusOK || w.Header().Get("HX-Redirect") != "/" || h.authenticationCookie(t).Value == old.Value {
					t.Fatalf("session did not rotate: %d %s", w.Code, w.Body.String())
				}

				called := false
				h.app.Web.Handle("GET /rotation-protected", authmiddleware.RequireAuthentication(h.accounts, h.app.ContentManager)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					called = true
					if got := user.GetUserFromContext(r.Context()); got == nil || got.ID != u.ID || (operation == "verification" && !got.Verified) {
						t.Fatal("replacement lost account identity or privileges")
					}
					w.WriteHeader(http.StatusNoContent)
				})))
				w = h.request(http.MethodGet, "/rotation-protected", nil, true)
				if called == failed || (!failed && w.Code != http.StatusNoContent) {
					t.Fatal("current session has incorrect access")
				}
				base, _ := url.Parse(h.baseURL)
				h.cookies.SetCookies(base, []*http.Cookie{old})
				called = false
				w = h.request(http.MethodGet, "/rotation-protected", nil, true)
				if called || w.Header().Get("HX-Redirect") != "/login" {
					t.Fatal("old cookie has incorrect access after rotation")
				}
			})
		})
	}
}

func TestHTTPVerificationSurvivesRotationFailure(t *testing.T) {
	for _, mode := range []string{"native", "htmx"} {
		t.Run(mode, func(t *testing.T) {
			runSiteScenario(t, func(h *httpHarness) {
				u := h.seedUser(t, "verification-rotation@example.invalid")
				u.Verified, u.VerificationRequired = false, true
				if err := h.accounts.Update(context.Background(), u); err != nil {
					t.Fatal(err)
				}
				h.establishSession(t, u)
				old := h.authenticationCookie(t)
				token, err := user.NewAuthVerificationToken(u, []byte(h.app.Config.Auth.JwtKey), time.Hour)
				if err != nil {
					t.Fatal(err)
				}
				if err := h.accounts.InsertToken(t.Context(), token.Metadata()); err != nil {
					t.Fatal(err)
				}
				if _, err := h.db.Exec(`CREATE TRIGGER reject_rotation BEFORE INSERT ON session BEGIN SELECT RAISE(ABORT, 'private rotation failure'); END`); err != nil {
					t.Fatal(err)
				}

				htmx := mode == "htmx"
				w := h.request(http.MethodPost, "/auth/verify", url.Values{"token": {token.Token}}, htmx)
				if w.Code != http.StatusInternalServerError || w.Body.String() != "Your account was verified, but the session could not be renewed. Please log in again.\n" {
					t.Fatalf("partial failure response = %d %q", w.Code, w.Body.String())
				}
				if !strings.Contains(h.logs.String(), "authenticated session could not be renewed") || strings.Contains(h.logs.String(), "private rotation failure") {
					t.Fatal("session renewal failure was not logged safely")
				}
				if len(w.Result().Cookies()) != 0 || w.Header().Get("HX-Redirect") != "" || w.Header().Get("Location") != "" {
					t.Fatal("failed renewal issued cookies or redirected")
				}
				stored, err := h.accounts.GetByID(t.Context(), u.ID)
				if err != nil || !stored.Verified || stored.SessionVersion != u.SessionVersion+1 {
					t.Fatalf("session failure rolled back verification: %+v, %v", stored, err)
				}
				if _, err := h.accounts.GetToken(t.Context(), token.TokenHash, "auth-verification"); !errors.Is(err, user.ErrTokenNotFound) {
					t.Fatalf("verification token was not consumed: %v", err)
				}
				if _, err := h.db.Exec(`DROP TRIGGER reject_rotation`); err != nil {
					t.Fatal(err)
				}

				called := false
				h.app.Web.Handle("GET /verification-protected", authmiddleware.RequireAuthentication(h.accounts, h.app.ContentManager)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					called = true
					account := user.GetUserFromContext(r.Context())
					if account == nil || account.ID != u.ID || !account.Verified {
						t.Fatal("fresh login did not restore the verified account")
					}
					w.WriteHeader(http.StatusNoContent)
				})))
				base, _ := url.Parse(h.baseURL)
				h.cookies.SetCookies(base, []*http.Cookie{old})
				w = h.request(http.MethodGet, "/verification-protected", nil, true)
				if called || w.Header().Get("HX-Redirect") != "/login" {
					t.Fatal("old cookie retained access after verification")
				}
				w = h.request(http.MethodPost, "/auth/verify", url.Values{"token": {token.Token}}, htmx)
				if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "Verification failed") || w.Header().Get("HX-Redirect") != "" {
					t.Fatalf("consumed verification token was not rejected: %d %s", w.Code, w.Body.String())
				}
				h.assertUserUnchanged(t, stored)

				w = h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {u.Email}, "password": {"TestPassword1!"}}, true)
				if !strings.HasSuffix(w.Header().Get("HX-Redirect"), "/verification-protected") || h.authenticationCookie(t).Value == old.Value {
					t.Fatalf("fresh login failed: %d %s", w.Code, w.Body.String())
				}
				if w = h.request(http.MethodGet, "/verification-protected", nil, htmx); w.Code != http.StatusNoContent || !called {
					t.Fatal("fresh login did not restore protected access")
				}
			})
		})
	}
}

func TestHTTPPasswordChangesRevokeAllSessions(t *testing.T) {
	for _, operation := range []string{"reset", "change", "failed reset", "failed change"} {
		t.Run(operation, func(t *testing.T) {
			runSiteScenario(t, func(h *httpHarness) {
				u := h.seedUser(t, "password-session@example.invalid")
				other := h.seedUser(t, "unrelated@example.invalid")
				h.establishSession(t, other)
				unrelated := h.authenticationCookie(t)
				h.establishSession(t, u)
				first := h.authenticationCookie(t)
				h.establishSession(t, u)
				second := h.authenticationCookie(t)
				failed := strings.HasPrefix(operation, "failed")
				if failed {
					if _, err := h.db.Exec(`CREATE TRIGGER reject_password BEFORE UPDATE ON user BEGIN SELECT RAISE(ABORT, 'password write failed'); END`); err != nil {
						t.Fatal(err)
					}
				}
				if strings.HasSuffix(operation, "reset") {
					base, _ := url.Parse(h.baseURL)
					h.cookies.SetCookies(base, []*http.Cookie{{Name: "auth-user-session", Path: "/", MaxAge: -1}})
					token, err := user.NewAuthResetToken(u, []byte(h.app.Config.Auth.JwtKey), time.Hour)
					if err != nil {
						t.Fatal(err)
					}
					if err := h.accounts.InsertToken(t.Context(), token.Metadata()); err != nil {
						t.Fatal(err)
					}
					h.request(http.MethodPost, "/auth/reset/email?token="+url.QueryEscape(token.Token), url.Values{"password": {"NewPassword1!"}, "passwordMatch": {"NewPassword1!"}}, true)
				} else {
					h.request(http.MethodPost, "/auth/change/email", url.Values{"oldPassword": {"TestPassword1!"}, "newPassword": {"NewPassword1!"}, "newPasswordMatch": {"NewPassword1!"}}, true)
				}
				stored, err := h.accounts.GetByID(t.Context(), u.ID)
				if err != nil || (stored.SessionVersion == u.SessionVersion) != failed {
					t.Fatalf("wrong account version after password operation: %+v, %v", stored, err)
				}
				called := false
				h.app.Web.Handle("GET /password-protected", authmiddleware.RequireAuthentication(h.accounts, h.app.ContentManager)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					called = true
					w.WriteHeader(http.StatusNoContent)
				})))
				base, _ := url.Parse(h.baseURL)
				for _, cookie := range []*http.Cookie{first, second, unrelated} {
					h.cookies.SetCookies(base, []*http.Cookie{cookie})
					called = false
					w := h.request(http.MethodGet, "/password-protected", nil, true)
					allowed := failed || cookie == unrelated
					if called != allowed || (!allowed && w.Header().Get("HX-Redirect") != "/login") {
						t.Fatalf("captured cookie access = %v, want %v", called, allowed)
					}
				}
				if !failed {
					h.cookies.SetCookies(base, []*http.Cookie{first})
					w := h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {u.Email}, "password": {"NewPassword1!"}}, true)
					if redirect := w.Header().Get("HX-Redirect"); w.Code != http.StatusOK || !strings.HasSuffix(redirect, "/password-protected") {
						t.Fatalf("new credentials could not establish a session: %d, redirect=%q, %s", w.Code, redirect, w.Body.String())
					}
					called = false
					if w = h.request(http.MethodGet, "/password-protected", nil, true); w.Code != http.StatusNoContent || !called {
						t.Fatal("new credentials did not restore protected access")
					}
				}
			}, func(cfg *config.Config) {
				// Revocation must not require a shared account/session transaction.
				cfg.HTTP.Session.Stores["sqliteStore"]["connection"] = filepath.Join(filepath.Dir(cfg.Database.Connection), "sessions.db")
			})
		})
	}
}

func TestHTTPAuthenticationRejectsOldSessionVersions(t *testing.T) {
	for _, scenario := range []string{"missing version", "wrong version type", "stale login snapshot", "account update"} {
		t.Run(scenario, func(t *testing.T) {
			runSiteScenario(t, func(h *httpHarness) {
				u := h.seedUser(t, "version@example.invalid")
				if scenario == "account update" {
					h.establishSession(t, u)
					u.Email = "updated@example.invalid"
					if err := h.accounts.Update(context.Background(), u); err != nil {
						t.Fatal(err)
					}
				} else if scenario == "stale login snapshot" {
					updated := *u
					if err := h.accounts.ChangePassword(context.Background(), &updated, "different hash"); err != nil {
						t.Fatal(err)
					}
					// Credentials were checked before the password changed.
					h.establishSession(t, u)
				} else {
					r := session.AddSessionManagerToRequestContext(httptest.NewRequest(http.MethodGet, "/", nil), h.sessions)
					s, err := session.New(r, "auth-user-session")
					if err != nil {
						t.Fatal(err)
					}
					s.Data["auth-user-session"] = u.ID
					if scenario == "wrong version type" {
						s.Data["auth-session-version"] = "1"
					}
					w := httptest.NewRecorder()
					if err := s.Save(w, r); err != nil {
						t.Fatal(err)
					}
					base, _ := url.Parse(h.baseURL)
					h.cookies.SetCookies(base, w.Result().Cookies())
				}
				h.app.Web.Handle("GET /version-protected", authmiddleware.RequireAuthentication(h.accounts, h.app.ContentManager)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					t.Fatal("stale or unversioned session authenticated")
				})))
				w := h.request(http.MethodGet, "/version-protected", nil, true)
				if w.Code != http.StatusOK || w.Header().Get("HX-Redirect") != "/login" {
					t.Fatalf("invalidated session response = %d %s", w.Code, w.Body.String())
				}
			})
		})
	}
}

func TestHTTPSQLiteSessionRevocation(t *testing.T) {
	for _, scenario := range []string{"logout", "stored expiry", "failed logout"} {
		t.Run(scenario, func(t *testing.T) {
			runSiteScenario(t, func(h *httpHarness) {
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
				claims := &sessionCookieClaims{}
				if _, err := jwt.ParseWithClaims(captured.Value, claims, func(*jwt.Token) (any, error) { return []byte(h.app.Config.HTTP.Session.JwtKey), nil }); err != nil {
					t.Fatal(err)
				}
				called := false
				h.app.Web.Handle("GET /revocation-protected", authmiddleware.RequireAuthentication(h.accounts, h.app.ContentManager)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
					u, err := identity.GetAuthenticatedUser(r, h.accounts)
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
					if _, err := h.db.Exec(`UPDATE session SET expires_at = ? WHERE id = ?`, time.Now().Add(-time.Minute), claims.ID); err != nil {
						t.Fatal(err)
					}
				} else {
					if failLogout {
						if _, err := h.db.Exec(`CREATE TRIGGER reject_logout BEFORE DELETE ON session BEGIN SELECT RAISE(ABORT, 'logout storage failure'); END`); err != nil {
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
						if _, err := h.db.Exec(`DROP TRIGGER reject_logout`); err != nil {
							t.Fatal(err)
						}
						failLogout = false
					}
					w = h.request(http.MethodPost, "/logout-observed", nil, true)
					if w.Code != http.StatusOK || w.Header().Get("HX-Redirect") != "/" {
						t.Fatalf("logout failed: %d %s", w.Code, w.Body.String())
					}
					var count int
					if err := h.db.QueryRowContext(t.Context(), `SELECT count(*) FROM session WHERE id = ?`, claims.ID).Scan(&count); err != nil || count != 0 {
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
