package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestHTTPAccountLookupCancellation(t *testing.T) {
	for _, workflow := range []string{"login", "reset form", "reset submission"} {
		for _, htmx := range []bool{false, true} {
			name := workflow + "/native"
			if htmx {
				name = workflow + "/htmx"
			}
			t.Run(name, func(t *testing.T) {
				runHTTPScenario(t, func(h *httpHarness) {
					u := h.seedUser(t, "person@example.invalid")
					method, path := http.MethodPost, "/auth/login/email"
					values := url.Values{"email": {u.Email}, "password": {"TestPassword1!"}}
					var tokenHash string
					if workflow != "login" {
						token, err := user.NewAuthResetToken(u, []byte(h.app.Config.Auth.JwtKey), time.Hour)
						if err != nil {
							t.Fatal(err)
						}
						if err := h.app.AccountRepository.InsertToken(t.Context(), token.Metadata()); err != nil {
							t.Fatal(err)
						}
						tokenHash = token.TokenHash
						path = "/auth/reset/email?token=" + url.QueryEscape(token.Token)
						values = url.Values{"password": {"NewPassword1!"}, "passwordMatch": {"NewPassword1!"}}
						if workflow == "reset form" {
							method, values = http.MethodGet, nil
						}
					}

					// Holding the only connection makes the real repository lookup wait.
					h.app.Database.SetMaxOpenConns(1)
					conn, err := h.app.Database.Conn(t.Context())
					if err != nil {
						t.Fatal(err)
					}
					defer conn.Close()
					synctest.Test(t, func(t *testing.T) {
						ctx, cancel := context.WithCancel(t.Context())
						defer cancel()
						r := httptest.NewRequest(method, path, strings.NewReader(values.Encode())).WithContext(ctx)
						r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
						if htmx {
							r.Header.Set("HX-Request", "true")
						}
						w := httptest.NewRecorder()
						done := make(chan struct{})
						waits := h.app.Database.Stats().WaitCount
						go func() {
							defer close(done)
							h.handler.ServeHTTP(w, r)
						}()
						synctest.Wait()
						if h.app.Database.Stats().WaitCount != waits+1 {
							t.Error("request did not reach the blocked storage lookup")
						}
						select {
						case <-done:
							t.Fatal("request completed before storage lookup was canceled")
						default:
						}

						cancel()
						synctest.Wait()
						select {
						case <-done:
						default:
							// Release a broken implementation before reporting the failure.
							conn.Close()
							<-done
							t.Fatal("request cancellation did not interrupt the storage lookup")
						}
						if w.Header().Get("Location") != "" || w.Header().Get("HX-Redirect") != "" {
							t.Fatal("canceled lookup redirected as a successful request")
						}
						for _, cookie := range w.Result().Cookies() {
							if cookie.Name == "auth-user-session" && cookie.Value != "" {
								t.Fatal("canceled lookup created an authenticated session")
							}
						}
					})
					if err := conn.Close(); err != nil {
						t.Fatal(err)
					}

					h.assertUserUnchanged(t, u)
					if tokenHash != "" {
						if _, err := h.app.AccountRepository.GetToken(t.Context(), tokenHash, user.ResetTokenPurpose); err != nil {
							t.Fatalf("canceled lookup consumed the reset token: %v", err)
						}
					}
					if len(h.mail.snapshot()) != 0 {
						t.Fatal("canceled lookup sent email")
					}
				})
			})
		}
	}
}
