package web_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	authmiddleware "github.com/trentnix/hyperserver/auth/middleware"
	"github.com/trentnix/hyperserver/auth/password"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestHTTPNavigationLogout(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(fmt.Sprintf("pending=%t", pending), func(t *testing.T) {
			runSiteScenario(t, func(h *httpHarness) {
				for _, path := range []string{"/", "/login", "/auth/login/email"} {
					w := h.request(http.MethodGet, path, nil, false)
					if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `<a href="/login">Log in</a>`) || strings.Contains(w.Body.String(), `action="/auth/logout"`) {
						t.Fatalf("%s did not show only the login action to a signed-out visitor", path)
					}
				}
				u := h.seedUser(t, "navigation@example.invalid")
				u.Verified, u.VerificationRequired = !pending, true
				if err := h.accounts.Update(t.Context(), u); err != nil {
					t.Fatal(err)
				}
				h.establishSession(t, u)
				paths := []string{"/"}
				if pending {
					paths = append(paths, "/auth/request/verify")
				}
				for _, path := range paths {
					w := h.request(http.MethodGet, path, nil, false)
					nav := regexp.MustCompile(`(?s)<nav id="navbar">.*?</nav>`).FindString(w.Body.String())
					if w.Code != http.StatusOK || !strings.Contains(nav, `<form class="logout-form" method="post" action="/auth/logout">`) || !strings.Contains(nav, `<button type="submit">Log out</button>`) {
						t.Fatalf("%s is missing the native navigation logout form", path)
					}
					if strings.Contains(nav, ">Log in</a>") {
						t.Fatal("signed-in navigation still offered login")
					}
				}

				w := h.request(http.MethodPost, "/auth/logout", nil, false)
				assertRedirectResponse(t, w, "/", false)
				h.assertRowCount(t, "session", 0)
				w = h.request(http.MethodGet, "/", nil, false)
				if w.Code != http.StatusOK || strings.Contains(w.Body.String(), u.Email) || !strings.Contains(w.Body.String(), ">Log in</a>") || strings.Contains(w.Body.String(), `action="/auth/logout"`) {
					t.Fatal("logout did not return to an anonymous home page")
				}
			})
		})
	}
}

func TestSiteNavigationWithoutAuthentication(t *testing.T) {
	runSiteScenario(t, func(h *httpHarness) {
		w := h.request(http.MethodGet, "/", nil, false)
		if w.Code != http.StatusOK || strings.Contains(w.Body.String(), ">Log in</a>") || strings.Contains(w.Body.String(), `action="/auth/logout"`) {
			t.Fatalf("disabled authentication: status %d, body %s", w.Code, w.Body.String())
		}
	}, func(cfg *config.Config) { cfg.Auth.Enabled = false })
}

func TestHTTPLoginRedirectDestinations(t *testing.T) {
	runSiteScenario(t, func(h *httpHarness) {
		u := h.seedUser(t, "redirect@example.invalid")
		for _, htmx := range []bool{false, true} {
			for _, tc := range []struct{ destination, home, want string }{
				{"", "/welcome", "/welcome"},
				{"/contact?from=login#form", "/welcome", "/contact?from=login#form"},
				{"https://attacker.invalid", "/welcome", "/welcome"},
				{h.baseURL + "/contact", "/welcome", "/welcome"},
				{"//attacker.invalid", "/welcome", "/welcome"},
				{`/\attacker.invalid`, "/welcome", "/welcome"},
				{"/page?x=%0a", "/welcome", "/welcome"},
				{"/bad%", "/welcome", "/welcome"},
				{"//attacker.invalid", "https://attacker.invalid", "/"},
			} {
				// Each login starts with a fresh browser, without a prior account session.
				jar, err := cookiejar.New(nil)
				if err != nil {
					t.Fatal(err)
				}
				h.cookies = jar
				r := session.AddSessionManagerToRequestContext(httptest.NewRequest(http.MethodGet, "/", nil), h.sessions)
				s, err := session.New(r, session.AuthSession)
				if err != nil {
					t.Fatal(err)
				}
				s.Data[session.RedirectURL] = tc.destination
				w := httptest.NewRecorder()
				if err := s.Save(w, r); err != nil {
					t.Fatal(err)
				}
				base, _ := url.Parse(h.baseURL)
				h.cookies.SetCookies(base, w.Result().Cookies())

				h.app.ContentManager.HomeURL = tc.home
				w = h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {u.Email}, "password": {"TestPassword1!"}}, htmx)
				assertRedirectResponse(t, w, tc.want, htmx)
				for _, cookie := range h.cookies.Cookies(base) {
					if cookie.Name == session.AuthSession {
						t.Fatal("login did not clear the return destination session")
					}
				}
				if home := h.request(http.MethodGet, "/", nil, false); !strings.Contains(home.Body.String(), u.Email) {
					t.Fatal("redirect selection lost the authenticated session")
				}
			}
		}
	})
}

func TestHTTPLoginReturnsToRequestedPath(t *testing.T) {
	for _, htmx := range []bool{false, true} {
		name := "ordinary"
		if htmx {
			name = "htmx"
		}
		t.Run(name, func(t *testing.T) {
			runSiteScenario(t, func(h *httpHarness) {
				u := h.seedUser(t, "return@example.invalid")
				called := false
				h.app.Web.Handle("GET /protected-return", authmiddleware.RequireAuthentication(h.accounts, h.app.ContentManager)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					called = true
					w.WriteHeader(http.StatusNoContent)
				})))
				// The harness uses absolute-form request targets. Save only path and query.
				path := "/protected-return?tab=settings"
				w := h.request(http.MethodGet, path, nil, htmx)
				assertRedirectResponse(t, w, "/login", htmx)
				if called {
					t.Fatal("anonymous request reached the protected handler")
				}
				w = h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {u.Email}, "password": {"TestPassword1!"}}, htmx)
				assertRedirectResponse(t, w, path, htmx)
				w = h.request(http.MethodGet, path, nil, false)
				if w.Code != http.StatusNoContent || !called {
					t.Fatal("login did not grant access to the original destination")
				}
			})
		})
	}
}

func TestHTTPLogoutRequiresPost(t *testing.T) {
	for _, path := range []string{"/auth/logout", "/logout"} {
		for _, htmx := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/htmx=%t", path, htmx), func(t *testing.T) {
				runSiteScenario(t, func(h *httpHarness) {
					u := h.seedUser(t, "logout-method@example.invalid")
					h.establishSession(t, u)
					captured := h.authenticationCookie(t)
					for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
						w := h.request(method, path, nil, htmx)
						// The site's catch-all serves 404 for an unmatched method.
						if w.Code != http.StatusNotFound && w.Code != http.StatusMethodNotAllowed {
							t.Fatalf("%s %s returned %d", method, path, w.Code)
						}
						if h.authenticationCookie(t).Value != captured.Value {
							t.Fatalf("%s changed the authentication cookie", method)
						}
						h.assertRowCount(t, "session", 1)
						if home := h.request(http.MethodGet, "/", nil, false); !strings.Contains(home.Body.String(), u.Email) {
							t.Fatalf("%s logged out the account", method)
						}
					}

					w := h.request(http.MethodPost, path, nil, htmx)
					if path == "/logout" {
						if w.Code != http.StatusTemporaryRedirect || w.Header().Get("Location") != "/auth/logout" || w.Header().Get("HX-Redirect") != "" {
							t.Fatalf("alias lost POST semantics: %d %v", w.Code, w.Header())
						}
						h.assertRowCount(t, "session", 1)
						// Follow 307 as a browser would: preserve the POST and HTMX header.
						w = h.request(http.MethodPost, w.Header().Get("Location"), nil, htmx)
					}
					assertRedirectResponse(t, w, "/", htmx)
					expired := false
					for _, cookie := range w.Result().Cookies() {
						if cookie.Name == captured.Name && cookie.MaxAge == -1 {
							expired = true
						}
					}
					if !expired {
						t.Fatal("POST logout did not expire the authentication cookie")
					}
					h.assertRowCount(t, "session", 0)
					h.assertUserUnchanged(t, u)
				})
			})
		}
	}
}

func TestHTTPReadRequestsDoNotSendMail(t *testing.T) {
	for _, authenticated := range []bool{false, true} {
		t.Run(fmt.Sprintf("authenticated=%t", authenticated), func(t *testing.T) {
			runSiteScenario(t, func(h *httpHarness) {
				u := h.seedUser(t, "read-only@example.invalid")
				u.Verified, u.VerificationRequired = false, true
				if err := h.accounts.Update(context.Background(), u); err != nil {
					t.Fatal(err)
				}
				u, err := h.accounts.GetByID(t.Context(), u.ID)
				if err != nil {
					t.Fatal(err)
				}
				if authenticated {
					h.establishSession(t, u)
				}
				for _, htmx := range []bool{false, true} {
					for _, path := range []string{
						"/test-email?to=" + url.QueryEscape(u.Email),
						"/auth/request/verify",
						"/auth/reset/request/email?email=" + url.QueryEscape(u.Email),
						"/auth/register/email?" + registrationForm().Encode(),
					} {
						for _, method := range []string{http.MethodGet, http.MethodHead} {
							h.request(method, path, nil, htmx)
							if len(h.mail.snapshot()) != 0 {
								t.Fatalf("%s %s sent mail", method, path)
							}
							h.assertRowCount(t, "usertoken", 0)
							h.assertRowCount(t, "user", 1)
							h.assertUserUnchanged(t, u)
						}
					}
				}
			})
		})
	}
}

func TestHTTPResetRequiresPasswordSubmission(t *testing.T) {
	runSiteScenario(t, func(h *httpHarness) {
		u := h.seedUser(t, "reset-method@example.invalid")
		token, err := user.NewAuthResetToken(u, []byte(h.app.Config.Auth.JwtKey), time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if err := h.accounts.InsertToken(t.Context(), token.Metadata()); err != nil {
			t.Fatal(err)
		}
		path := "/auth/reset/email?token=" + url.QueryEscape(token.Token)
		for _, htmx := range []bool{false, true} {
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				w := h.request(method, path, nil, htmx)
				if w.Code != http.StatusOK || w.Header().Get("HX-Redirect") != "" || w.Header().Get("Location") != "" {
					t.Fatalf("%s reset did not display a form: %d", method, w.Code)
				}
				if method == http.MethodGet {
					for _, field := range []string{`method="post"`, `action="` + path + `"`, `hx-post="` + path + `"`} {
						if !strings.Contains(w.Body.String(), field) {
							t.Fatalf("reset form missing %s", field)
						}
					}
				}
				h.assertUserUnchanged(t, u)
				if _, err := h.accounts.GetToken(t.Context(), token.TokenHash, "auth-reset"); err != nil {
					t.Fatalf("%s consumed the reset token: %v", method, err)
				}
			}
			// Posting the link without password fields must not consume the token.
			h.request(http.MethodPost, path, nil, htmx)
			h.assertUserUnchanged(t, u)
			h.assertRowCount(t, "usertoken", 1)
		}
		h.request(http.MethodPost, path, url.Values{"password": {"NewPassword1!"}, "passwordMatch": {"NewPassword1!"}}, true)
		stored, err := h.accounts.GetByID(t.Context(), u.ID)
		if err != nil || !password.CheckPasswordHash("NewPassword1!", stored.Password) {
			t.Fatalf("password submission did not reset credentials: %v", err)
		}
		h.assertRowCount(t, "usertoken", 0)
		if len(h.mail.snapshot()) != 0 {
			t.Fatal("reset form or redemption sent mail")
		}
	})
}
