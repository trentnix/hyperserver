package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/middleware"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func useSQLiteSessions(cfg *config.Config) {
	cfg.HTTP.Session.Stores = map[string]map[string]string{"sqliteStore": {"enabled": "true", "connection": cfg.Database.Connection, "sessiontable": "session"}}
	cfg.HTTP.Session.Types["default"] = "sqliteStore"
}

func TestSQLiteSessionLoadRetry(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		const name = "retry-session"
		setup := session.AddSessionManagerToRequestContext(httptest.NewRequest(http.MethodGet, "/", nil), h.app.SessionManager)
		original, err := session.New(setup, name)
		if err != nil {
			t.Fatal(err)
		}
		original.Data["marker"] = "preserved"
		w := httptest.NewRecorder()
		if err := original.Save(w, setup); err != nil {
			t.Fatal(err)
		}
		r := session.AddSessionManagerToRequestContext(httptest.NewRequest(http.MethodGet, "/", nil), h.app.SessionManager)
		for _, cookie := range w.Result().Cookies() {
			r.AddCookie(cookie)
		}

		if _, err := h.app.Database.Exec(`ALTER TABLE session RENAME TO unavailable_session`); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if loaded, err := session.Get(r, name); loaded != nil || err == nil {
				t.Fatalf("unavailable storage returned %v, %v, want nil and a load error", loaded, err)
			}
		}
		if _, err := h.app.Database.Exec(`ALTER TABLE unavailable_session RENAME TO session`); err != nil {
			t.Fatal(err)
		}

		loaded, err := session.Get(r, name)
		if err != nil || loaded == nil {
			t.Fatalf("retry after restoring storage failed: %v", err)
		}
		if loaded.IsNew || loaded.ID != original.ID || loaded.Data["marker"] != "preserved" {
			t.Fatalf("retry did not restore the original session: %+v", loaded)
		}
		if cached, err := session.Get(r, name); err != nil || cached != loaded {
			t.Fatalf("successful retry was not cached: %v", err)
		}
	}, useSQLiteSessions)
}

func TestHTTPProtectedRouteSessionExpiry(t *testing.T) {
	// Authentication always uses SQLite. Exercise both default providers for other sessions.
	for _, provider := range []string{"cookieStore", "sqliteStore"} {
		t.Run(provider, func(t *testing.T) {
			runHTTPScenario(t, func(h *httpHarness) {
				account := h.seedUser(t, "session-user@example.invalid")
				called := false
				h.app.Web.Handle("GET /protected-session-check", middleware.RequireAuthentication(h.app.AccountReader, h.app.ContentManager)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					called = true
					u := user.GetUserFromContext(r.Context())
					if u == nil || u.ID != account.ID {
						t.Fatalf("protected handler received the wrong account: %+v", u)
					}
					w.WriteHeader(http.StatusNoContent)
				})))
				w := h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {account.Email}, "password": {"TestPassword1!"}}, true)
				if w.Code != http.StatusOK || w.Header().Get("HX-Redirect") != "/" {
					t.Fatalf("login failed: %d %s", w.Code, w.Body.String())
				}
				w = h.request(http.MethodGet, "/protected-session-check", nil, true)
				if w.Code != http.StatusNoContent || !called {
					t.Fatalf("valid session denied access: %d %s", w.Code, w.Body.String())
				}

				base, err := url.Parse(h.baseURL)
				if err != nil {
					t.Fatal(err)
				}
				var authCookie *http.Cookie
				for _, cookie := range h.cookies.Cookies(base) {
					if cookie.Name == "auth-user-session" {
						authCookie = cookie
					}
				}
				if authCookie == nil {
					t.Fatal("login did not issue an authentication cookie")
				}
				key := []byte(h.app.Config.HTTP.Session.JwtKey)
				claims := &session.SessionClaims{}
				if _, err := jwt.ParseWithClaims(authCookie.Value, claims, func(*jwt.Token) (any, error) { return key, nil }); err != nil {
					t.Fatal(err)
				}
				// Expire only the signed token. Keep the browser cookie and stored data.
				claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Hour))
				authCookie.Value, err = jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(key)
				if err != nil {
					t.Fatal(err)
				}
				authCookie.Path = "/"
				h.cookies.SetCookies(base, []*http.Cookie{authCookie})

				called = false
				w = h.request(http.MethodGet, "/protected-session-check", nil, true)
				if called || w.Code != http.StatusOK || w.Header().Get("HX-Redirect") != "/login" {
					t.Fatalf("expired session did not redirect without running the handler: called=%v, status=%d, redirect=%q", called, w.Code, w.Header().Get("HX-Redirect"))
				}
				h.assertUserUnchanged(t, account)
			}, func(cfg *config.Config) {
				if provider == "sqliteStore" {
					useSQLiteSessions(cfg)
				}
			})
		})
	}
}

func TestHTTPSessionLoading(t *testing.T) {
	for _, provider := range []string{"cookieStore", "sqliteStore"} {
		for _, outcome := range []string{"missing cookie", "valid", "expired", "invalid signature", "expired invalid signature", "malformed", "empty cookie", "oversized cookie", "oversized data", "corrupt base64", "corrupt JSON", "empty data", "missing row", "unavailable storage"} {
			if provider == "cookieStore" && (outcome == "missing row" || outcome == "unavailable storage") {
				continue
			}
			t.Run(provider+"/"+outcome, func(t *testing.T) {
				runHTTPScenario(t, func(h *httpHarness) {
					const name = "auth-user-session"
					// Exercise raw cookie loading too, without authenticating an account.
					h.app.SessionManager.Types[name] = provider
					encoded, err := (&session.Session{Data: map[string]any{"marker": "stored"}}).EncodedData()
					if err != nil {
						t.Fatal(err)
					}
					switch outcome {
					case "corrupt base64":
						encoded = "not base64!"
					case "corrupt JSON":
						encoded = "aW52YWxpZA=="
					case "empty data":
						encoded = ""
					case "oversized data":
						encoded = strings.Repeat("a", session.MaxSessionDataSize*2)
					}
					expires := time.Now().Add(time.Hour)
					if strings.HasPrefix(outcome, "expired") {
						expires = time.Now().Add(-time.Hour)
					}
					claims := session.SessionClaims{
						ID: "stored-session", Purpose: "session", Value: encoded,
						RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(expires)},
					}
					if provider == "sqliteStore" {
						if outcome != "missing row" {
							_, err := h.app.Database.Exec(`INSERT INTO session (id, session, expires_at) VALUES (?, ?, ?)`, claims.ID, encoded, time.Now().Add(time.Hour))
							if err != nil {
								t.Fatal(err)
							}
						}
						if outcome == "unavailable storage" {
							if _, err := h.app.Database.Exec(`ALTER TABLE session RENAME TO unavailable_session`); err != nil {
								t.Fatal(err)
							}
						}
						claims.Value = ""
					}
					key := []byte(h.app.Config.HTTP.Session.JwtKey)
					if strings.Contains(outcome, "invalid signature") {
						key = []byte("wrong-signing-key")
					}
					raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(key)
					if err != nil {
						t.Fatal(err)
					}
					if outcome == "malformed" {
						raw = "not-a-token"
					} else if outcome == "empty cookie" {
						raw = ""
					} else if outcome == "oversized cookie" {
						raw = strings.Repeat("a", session.MaxCookieSize+1)
					}
					if outcome != "missing cookie" {
						base, err := url.Parse(h.baseURL)
						if err != nil {
							t.Fatal(err)
						}
						h.cookies.SetCookies(base, []*http.Cookie{{Name: name, Value: raw, Path: "/"}})
					}

					wantNew := outcome == "missing cookie" || outcome == "expired" || outcome == "missing row"
					wantSuccess := wantNew || outcome == "valid"
					called := false
					h.app.Web.Handle("GET /session-check", middleware.RequireAnonymous(h.app.AccountReader, h.app.ContentManager)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						called = true
						s, err := session.Get(r, name)
						if err != nil || s == nil {
							t.Fatalf("session unavailable in handler: %v", err)
						}
						if s.IsNew != wantNew || (wantNew && (len(s.Data) != 0 || s.ID == claims.ID)) || (!wantNew && s.Data["marker"] != "stored") {
							t.Fatalf("unexpected session: %+v", s)
						}
						w.WriteHeader(http.StatusNoContent)
					})))
					w := h.request(http.MethodGet, "/session-check", nil, false)
					wantStatus := http.StatusInternalServerError
					if wantSuccess {
						wantStatus = http.StatusNoContent
					} else if strings.Contains(outcome, "invalid signature") || outcome == "malformed" || outcome == "empty cookie" || outcome == "oversized cookie" || (outcome == "oversized data" && provider == "cookieStore") {
						wantStatus = http.StatusBadRequest
					}
					if w.Code != wantStatus || called != wantSuccess {
						t.Fatalf("status=%d, handler called=%v, want %d, %v: %s", w.Code, called, wantStatus, wantSuccess, w.Body.String())
					}
					cookies := w.Result().Cookies()
					if wantStatus == http.StatusBadRequest {
						if len(cookies) != 1 || cookies[0].Name != name || cookies[0].Value != "" || cookies[0].MaxAge != -1 || cookies[0].Path != "/" || !cookies[0].HttpOnly {
							t.Fatalf("invalid cookie was not expired: %v", cookies)
						}
					} else if len(cookies) != 0 {
						t.Fatal("session loading changed cookies without an invalid token")
					}
					if w.Header().Get("X-Request-ID") == "" {
						t.Fatal("session loading ran before request logging")
					}
					if !wantSuccess {
						if !strings.Contains(h.logs.String(), "Request Error") {
							t.Fatal("session load failure was not logged")
						}
						wantBody := "There was an error loading the authenticated user\n"
						if wantStatus == http.StatusBadRequest {
							wantBody = "Invalid session cookie\n"
						}
						if w.Body.String() != wantBody {
							t.Fatalf("unexpected error response: %q", w.Body.String())
						}
					}
				}, func(cfg *config.Config) {
					if provider == "sqliteStore" {
						useSQLiteSessions(cfg)
					}
				})
			})
		}
	}
}

func TestHTTPInvalidAuthCookieRecovery(t *testing.T) {
	for _, provider := range []string{"cookieStore", "sqliteStore"} {
		for _, logging := range []bool{false, true} {
			name := provider + "/without logger"
			if logging {
				name = provider + "/with logger"
			}
			t.Run(name, func(t *testing.T) {
				runHTTPScenario(t, func(h *httpHarness) {
					if !logging {
						h.handler = middleware.LoadSessionManagement(h.app.AccountReader, h.app.SessionManager)(h.app.Web)
					}
					base, err := url.Parse(h.baseURL)
					if err != nil {
						t.Fatal(err)
					}
					h.cookies.SetCookies(base, []*http.Cookie{
						{Name: "auth-user-session", Value: "invalid.jwt.token", Path: "/"},
						{Name: "unrelated", Value: "preserved", Path: "/"},
					})
					w := h.request(http.MethodGet, "/login", nil, false)
					if w.Code != http.StatusBadRequest || w.Body.String() != "Invalid session cookie\n" {
						t.Fatalf("invalid cookie response = %d %q", w.Code, w.Body.String())
					}
					cookies := h.cookies.Cookies(base)
					if len(cookies) != 1 || cookies[0].Name != "unrelated" || cookies[0].Value != "preserved" {
						t.Fatalf("cookie cleanup changed the wrong cookies: %v", cookies)
					}
					w = h.request(http.MethodGet, "/login", nil, false)
					if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `hx-get="/auth/login/email"`) {
						t.Fatalf("login page did not recover: %d %s", w.Code, w.Body.String())
					}
				}, func(cfg *config.Config) {
					if provider == "sqliteStore" {
						useSQLiteSessions(cfg)
					}
				})
			})
		}
	}
}

func TestHTTPLoginRedirectSessionFailure(t *testing.T) {
	for _, provider := range []string{"cookieStore", "sqliteStore"} {
		for _, failure := range []string{"invalid cookie", "corrupt data", "storage failure"} {
			if provider == "cookieStore" && failure == "storage failure" {
				continue
			}
			t.Run(provider+"/"+failure, func(t *testing.T) {
				runHTTPScenario(t, func(h *httpHarness) {
					account := h.seedUser(t, "redirect-session@example.invalid")
					r := session.AddSessionManagerToRequestContext(httptest.NewRequest(http.MethodGet, "/", nil), h.app.SessionManager)
					redirect, err := session.New(r, session.AuthSession)
					if err != nil {
						t.Fatal(err)
					}
					redirect.Data[session.RedirectURL] = "/about"
					recorder := httptest.NewRecorder()
					if err := redirect.Save(recorder, r); err != nil {
						t.Fatal(err)
					}
					cookie := recorder.Result().Cookies()[0]
					switch failure {
					case "invalid cookie":
						cookie.Value = "invalid.jwt.token"
					case "corrupt data":
						if provider == "sqliteStore" {
							if _, err := h.app.Database.Exec(`UPDATE session SET session = 'invalid base64!' WHERE id = ?`, redirect.ID); err != nil {
								t.Fatal(err)
							}
						} else {
							claims := session.SessionClaims{ID: redirect.ID, Purpose: "session", Value: "invalid base64!", RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}}
							cookie.Value, err = jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(h.app.Config.HTTP.Session.JwtKey))
							if err != nil {
								t.Fatal(err)
							}
						}
					case "storage failure":
						if _, err := h.app.Database.Exec(`ALTER TABLE session RENAME TO unavailable_session`); err != nil {
							t.Fatal(err)
						}
					}
					base, err := url.Parse(h.baseURL)
					if err != nil {
						t.Fatal(err)
					}
					h.cookies.SetCookies(base, []*http.Cookie{cookie})

					form := url.Values{"email": {account.Email}, "password": {"TestPassword1!"}}
					w := h.request(http.MethodPost, "/auth/login/email", form, true)
					wantStatus, wantBody := http.StatusInternalServerError, "Unable to load the login session\n"
					if failure == "invalid cookie" {
						wantStatus, wantBody = http.StatusBadRequest, "Invalid session cookie\n"
					}
					if w.Code != wantStatus || w.Body.String() != wantBody || w.Header().Get("HX-Redirect") != "" {
						t.Fatalf("login failure = %d %q, redirect=%q", w.Code, w.Body.String(), w.Header().Get("HX-Redirect"))
					}
					cookies := w.Result().Cookies()
					if failure == "invalid cookie" {
						if len(cookies) != 1 || cookies[0].Name != session.AuthSession || cookies[0].MaxAge != -1 {
							t.Fatalf("login did not expire only the invalid redirect cookie: %v", cookies)
						}
						w = h.request(http.MethodPost, "/auth/login/email", form, true)
						if w.Code != http.StatusOK || w.Header().Get("HX-Redirect") != "/" {
							t.Fatalf("login did not recover: %d %s", w.Code, w.Body.String())
						}
					} else if len(cookies) != 0 {
						t.Fatalf("failed login issued or cleared cookies: %v", cookies)
					}
					h.assertUserUnchanged(t, account)
				}, func(cfg *config.Config) {
					if provider == "sqliteStore" {
						// Only redirect sessions use SQLite, so failure reaches the login handler.
						useSQLiteSessions(cfg)
						cfg.HTTP.Session.Stores["cookieStore"] = map[string]string{"enabled": "true"}
						cfg.HTTP.Session.Types["default"] = "cookieStore"
						cfg.HTTP.Session.Types[session.AuthSession] = "sqliteStore"
					}
				})
			})
		}
	}
}
