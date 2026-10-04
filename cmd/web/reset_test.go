package main

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/auth/password"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestHTTPResetPasswordPolicy(t *testing.T) {
	for _, requireNew := range []bool{false, true} {
		name := "reuse allowed"
		if requireNew {
			name = "new password required"
		}
		t.Run(name, func(t *testing.T) {
			runHTTPScenario(t, func(h *httpHarness) {
				for _, htmx := range []bool{false, true} {
					email := "native@example.invalid"
					if htmx {
						email = "htmx@example.invalid"
					}
					u := h.seedUser(t, email)
					token, err := user.NewAuthResetToken(u, []byte(h.app.Config.Auth.JwtKey), time.Hour)
					if err != nil {
						t.Fatal(err)
					}
					if err := token.Create(h.app.Database); err != nil {
						t.Fatal(err)
					}
					path := "/auth/reset/email?token=" + url.QueryEscape(token.Token)
					w := h.request(http.MethodPost, path, url.Values{"password": {"TestPassword1!"}, "passwordMatch": {"TestPassword1!"}}, htmx)
					if requireNew {
						assertFormRejected(t, w, "a new password is required")
						h.assertUserUnchanged(t, u)
						if _, err := user.GetAuthResetTokenByHash(h.app.Database, token.TokenHash); err != nil {
							t.Fatalf("rejected password consumed token: %v", err)
						}
						w = h.request(http.MethodPost, path, url.Values{"password": {"NewPassword1!"}, "passwordMatch": {"NewPassword1!"}}, htmx)
					}
					var missing *user.ErrTokenNotFound
					if _, err := user.GetAuthResetTokenByHash(h.app.Database, token.TokenHash); !errors.As(err, &missing) {
						t.Fatalf("accepted password did not consume token: %v", err)
					}
					assertRedirectResponse(t, w, "/login", htmx)
					stored, err := user.GetUserByID(h.app.Database, u.ID)
					wantPassword := "TestPassword1!"
					if requireNew {
						wantPassword = "NewPassword1!"
					}
					if err != nil || !password.CheckPasswordHash(wantPassword, stored.Password) {
						t.Fatalf("reset stored the wrong password: %v", err)
					}
				}
			}, func(c *config.Config) { c.Auth.ResetRequiresNewCredentials = requireNew })
		})
	}
}

func TestHTTPResetHashingFailurePreservesToken(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		u := h.seedUser(t, "person@example.invalid")
		token, err := user.NewAuthResetToken(u, []byte(h.app.Config.Auth.JwtKey), time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if err := token.Create(h.app.Database); err != nil {
			t.Fatal(err)
		}
		path := "/auth/reset/email?token=" + url.QueryEscape(token.Token)
		// This satisfies form complexity but exceeds bcrypt's 72-byte limit.
		tooLong := strings.Repeat("a", 70) + "1!a"
		for _, htmx := range []bool{false, true} {
			w := h.request(http.MethodPost, path, url.Values{"password": {tooLong}, "passwordMatch": {tooLong}}, htmx)
			assertFormRejected(t, w, "Unable to reset password")
			h.assertUserUnchanged(t, u)
			if _, err := user.GetAuthResetTokenByHash(h.app.Database, token.TokenHash); err != nil {
				t.Fatalf("hashing failure consumed token: %v", err)
			}
			if strings.Contains(h.logs.String(), tooLong) || strings.Contains(h.logs.String(), token.Token) {
				t.Fatal("hashing failure logged credentials")
			}
		}
		w := h.request(http.MethodPost, path, url.Values{"password": {"NewPassword1!"}, "passwordMatch": {"NewPassword1!"}}, true)
		if w.Header().Get("HX-Redirect") != "/login" {
			t.Fatal("retry with a valid password failed")
		}
	})
}

func TestHTTPResetConsumesExactTokenAndRejectsReplay(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		u := h.seedUser(t, "person@example.invalid")
		key := []byte(h.app.Config.Auth.JwtKey)
		first, err := user.NewAuthResetToken(u, key, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		second, err := user.NewAuthResetToken(u, key, 2*time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		for _, token := range []*user.AuthResetToken{first, second} {
			if err := token.Create(h.app.Database); err != nil {
				t.Fatal(err)
			}
		}
		// Redeem the token other than the one an account-only lookup selects.
		selected, err := user.GetAuthResetTokenByUser(h.app.Database, u.ID)
		if err != nil {
			t.Fatal(err)
		}
		presented, untouched := first, second
		if selected.TokenHash == first.TokenHash {
			presented, untouched = second, first
		}
		path := "/auth/reset/email?token=" + url.QueryEscape(presented.Token)
		w := h.request(http.MethodPost, path, url.Values{"password": {"NewPassword1!"}, "passwordMatch": {"NewPassword1!"}}, true)
		if w.Header().Get("HX-Redirect") != "/login" {
			t.Fatal("reset did not succeed")
		}
		var missing *user.ErrTokenNotFound
		if _, err := user.GetAuthResetTokenByHash(h.app.Database, presented.TokenHash); !errors.As(err, &missing) {
			t.Fatalf("presented token was not consumed: %v", err)
		}
		if _, err := user.GetAuthResetTokenByHash(h.app.Database, untouched.TokenHash); err != nil {
			t.Fatalf("different token was consumed: %v", err)
		}
		for _, htmx := range []bool{false, true} {
			w := h.request(http.MethodPost, path, url.Values{"password": {"ReplayPassword1!"}, "passwordMatch": {"ReplayPassword1!"}}, htmx)
			if w.Header().Get("HX-Redirect") != "" {
				t.Fatal("replay returned a success redirect")
			}
			h.assertNoTokenExposure(t, w, presented.Token)
		}
		stored, err := user.GetUserByID(h.app.Database, u.ID)
		if err != nil || !password.CheckPasswordHash("NewPassword1!", stored.Password) {
			t.Fatalf("replay changed the password: %v", err)
		}
	})
}

func TestHTTPResetDeletionFailureRollsBackPassword(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		u := h.seedUser(t, "person@example.invalid")
		token, err := user.NewAuthResetToken(u, []byte(h.app.Config.Auth.JwtKey), time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if err := token.Create(h.app.Database); err != nil {
			t.Fatal(err)
		}
		if _, err := h.app.Database.Exec(`CREATE TRIGGER fail_reset BEFORE DELETE ON usertoken BEGIN SELECT RAISE(ABORT, 'failed deletion'); END`); err != nil {
			t.Fatal(err)
		}
		path := "/auth/reset/email?token=" + url.QueryEscape(token.Token)
		for _, htmx := range []bool{false, true} {
			w := h.request(http.MethodPost, path, url.Values{"password": {"NewPassword1!"}, "passwordMatch": {"NewPassword1!"}}, htmx)
			if w.Header().Get("HX-Redirect") != "" {
				t.Error("failed reset returned a success redirect")
			}
			h.assertUserUnchanged(t, u)
			if _, err := user.GetAuthResetTokenByHash(h.app.Database, token.TokenHash); err != nil {
				t.Fatalf("failed reset consumed token: %v", err)
			}
		}
	})
}
