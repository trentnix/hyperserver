package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/auth/password"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestHTTPLogoutRequiresPost(t *testing.T) {
	for _, path := range []string{"/auth/logout", "/logout"} {
		for _, htmx := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/htmx=%t", path, htmx), func(t *testing.T) {
				runHTTPScenario(t, func(h *httpHarness) {
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
			runHTTPScenario(t, func(h *httpHarness) {
				u := h.seedUser(t, "read-only@example.invalid")
				u.Verified, u.VerificationRequired = false, true
				if err := u.Update(context.Background(), h.app.Database); err != nil {
					t.Fatal(err)
				}
				u, err := user.GetUserByID(h.app.Database, u.ID)
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
	runHTTPScenario(t, func(h *httpHarness) {
		u := h.seedUser(t, "reset-method@example.invalid")
		token, err := user.NewAuthResetToken(u, []byte(h.app.Config.Auth.JwtKey), time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if err := token.Create(h.app.Database); err != nil {
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
				if _, err := user.GetAuthResetTokenByHash(h.app.Database, token.TokenHash); err != nil {
					t.Fatalf("%s consumed the reset token: %v", method, err)
				}
			}
			// Posting the link without password fields must not consume the token.
			h.request(http.MethodPost, path, nil, htmx)
			h.assertUserUnchanged(t, u)
			h.assertRowCount(t, "usertoken", 1)
		}
		h.request(http.MethodPost, path, url.Values{"password": {"NewPassword1!"}, "passwordMatch": {"NewPassword1!"}}, true)
		stored, err := user.GetUserByID(h.app.Database, u.ID)
		if err != nil || !password.CheckPasswordHash("NewPassword1!", stored.Password) {
			t.Fatalf("password submission did not reset credentials: %v", err)
		}
		h.assertRowCount(t, "usertoken", 0)
		if len(h.mail.snapshot()) != 0 {
			t.Fatal("reset form or redemption sent mail")
		}
	})
}
