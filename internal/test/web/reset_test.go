package web_test

import (
	"context"
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
					if err := h.accounts.InsertToken(t.Context(), token.Metadata()); err != nil {
						t.Fatal(err)
					}
					path := "/auth/reset/email?token=" + url.QueryEscape(token.Token)
					w := h.request(http.MethodPost, path, url.Values{"password": {"TestPassword1!"}, "passwordMatch": {"TestPassword1!"}}, htmx)
					if requireNew {
						assertFormRejected(t, w, "a new password is required")
						h.assertUserUnchanged(t, u)
						if _, err := h.accounts.GetToken(t.Context(), token.TokenHash, "auth-reset"); err != nil {
							t.Fatalf("rejected password consumed token: %v", err)
						}
						w = h.request(http.MethodPost, path, url.Values{"password": {"NewPassword1!"}, "passwordMatch": {"NewPassword1!"}}, htmx)
					}
					if _, err := h.accounts.GetToken(t.Context(), token.TokenHash, "auth-reset"); !errors.Is(err, user.ErrTokenNotFound) {
						t.Fatalf("accepted password did not consume token: %v", err)
					}
					assertRedirectResponse(t, w, "/auth/login/email", htmx)
					stored, err := h.accounts.GetByID(t.Context(), u.ID)
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

func TestHTTPResetOversizedPasswordPreservesToken(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		u := h.seedUser(t, "person@example.invalid")
		token, err := user.NewAuthResetToken(u, []byte(h.app.Config.Auth.JwtKey), time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if err := h.accounts.InsertToken(t.Context(), token.Metadata()); err != nil {
			t.Fatal(err)
		}
		path := "/auth/reset/email?token=" + url.QueryEscape(token.Token)
		// This satisfies the minimum and character rules but exceeds the byte limit.
		tooLong := strings.Repeat("a", 70) + "1!a"
		for _, htmx := range []bool{false, true} {
			w := h.request(http.MethodPost, path, url.Values{"password": {tooLong}, "passwordMatch": {tooLong}}, htmx)
			assertFormRejected(t, w, "password must not exceed 72 bytes in UTF-8")
			h.assertUserUnchanged(t, u)
			if _, err := h.accounts.GetToken(t.Context(), token.TokenHash, "auth-reset"); err != nil {
				t.Fatalf("invalid password consumed token: %v", err)
			}
			if strings.Contains(h.logs.String(), tooLong) || strings.Contains(h.logs.String(), token.Token) {
				t.Fatal("invalid password logged credentials")
			}
		}
		w := h.request(http.MethodPost, path, url.Values{"password": {"NewPassword1!"}, "passwordMatch": {"NewPassword1!"}}, true)
		if w.Header().Get("HX-Redirect") != "/auth/login/email" {
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
			if err := h.accounts.InsertToken(t.Context(), token.Metadata()); err != nil {
				t.Fatal(err)
			}
		}
		// Redeem the token other than the one an account-only lookup selects.
		var selectedHash string
		if err := h.db.QueryRowContext(t.Context(), "SELECT token_hash FROM usertoken WHERE user_id = ? AND token_type = ?", u.ID, "auth-reset").Scan(&selectedHash); err != nil {
			t.Fatal(err)
		}
		presented, untouched := first, second
		if selectedHash == first.TokenHash {
			presented, untouched = second, first
		}
		path := "/auth/reset/email?token=" + url.QueryEscape(presented.Token)
		w := h.request(http.MethodPost, path, url.Values{"password": {"NewPassword1!"}, "passwordMatch": {"NewPassword1!"}}, true)
		if w.Header().Get("HX-Redirect") != "/auth/login/email" {
			t.Fatal("reset did not succeed")
		}
		if _, err := h.accounts.GetToken(t.Context(), presented.TokenHash, "auth-reset"); !errors.Is(err, user.ErrTokenNotFound) {
			t.Fatalf("presented token was not consumed: %v", err)
		}
		if _, err := h.accounts.GetToken(t.Context(), untouched.TokenHash, "auth-reset"); err != nil {
			t.Fatalf("different token was consumed: %v", err)
		}
		for _, htmx := range []bool{false, true} {
			w := h.request(http.MethodPost, path, url.Values{"password": {"ReplayPassword1!"}, "passwordMatch": {"ReplayPassword1!"}}, htmx)
			if w.Code != http.StatusBadRequest || w.Header().Get("HX-Redirect") != "" {
				t.Fatal("replay returned a success redirect")
			}
			h.assertNoTokenExposure(t, w, presented.Token)
		}
		stored, err := h.accounts.GetByID(t.Context(), u.ID)
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
		if err := h.accounts.InsertToken(t.Context(), token.Metadata()); err != nil {
			t.Fatal(err)
		}
		if _, err := h.db.Exec(`CREATE TRIGGER fail_reset BEFORE DELETE ON usertoken BEGIN SELECT RAISE(ABORT, 'failed deletion'); END`); err != nil {
			t.Fatal(err)
		}
		path := "/auth/reset/email?token=" + url.QueryEscape(token.Token)
		for _, htmx := range []bool{false, true} {
			w := h.request(http.MethodPost, path, url.Values{"password": {"NewPassword1!"}, "passwordMatch": {"NewPassword1!"}}, htmx)
			if w.Code != http.StatusInternalServerError || w.Header().Get("HX-Redirect") != "" {
				t.Error("failed reset returned a success redirect")
			}
			h.assertUserUnchanged(t, u)
			if _, err := h.accounts.GetToken(t.Context(), token.TokenHash, "auth-reset"); err != nil {
				t.Fatalf("failed reset consumed token: %v", err)
			}
		}
	})
}

func TestHTTPRejectsWrongPurposeTokens(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		u := h.seedUser(t, "person@example.invalid")
		u.Verified, u.VerificationRequired = false, true
		if err := h.accounts.Update(context.Background(), u); err != nil {
			t.Fatal(err)
		}
		u, err := h.accounts.GetByID(t.Context(), u.ID)
		if err != nil {
			t.Fatal(err)
		}
		key := []byte(h.app.Config.Auth.JwtKey)
		reset, err := user.NewAuthResetToken(u, key, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if err := h.accounts.InsertToken(t.Context(), reset.Metadata()); err != nil {
			t.Fatal(err)
		}
		verification, err := user.NewAuthVerificationToken(u, key, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		for _, htmx := range []bool{false, true} {
			for _, tc := range []struct{ method, path, message string }{
				{http.MethodPost, "/auth/verify", "Verification failed"},
				{http.MethodGet, "/auth/reset/email?token=" + url.QueryEscape(verification.Token), "invalid or expired"},
				{http.MethodPost, "/auth/reset/email?token=" + url.QueryEscape(verification.Token), "invalid or expired"},
			} {
				form := url.Values{"password": {"NewPassword1!"}, "passwordMatch": {"NewPassword1!"}}
				if tc.path == "/auth/verify" {
					form.Set("token", reset.Token)
				}
				w := h.request(tc.method, tc.path, form, htmx)
				if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), tc.message) || strings.Contains(w.Body.String(), `id="reset-password-form"`) || w.Header().Get("HX-Redirect") != "" {
					t.Fatal("wrong-purpose token did not produce a rejection")
				}
				h.assertUserUnchanged(t, u)
				h.assertRowCount(t, "usertoken", 1)
				h.assertNoTokenExposure(t, w, reset.Token)
				h.assertNoTokenExposure(t, w, verification.Token)
			}
		}
	})
}

func TestHTTPExpiredResetTokenIsBadRequest(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		u := h.seedUser(t, "person@example.invalid")
		token, err := user.NewAuthResetToken(u, []byte(h.app.Config.Auth.JwtKey), time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		token.ExpiresAt = time.Now().Add(-time.Hour)
		if err := h.accounts.InsertToken(t.Context(), token.Metadata()); err != nil {
			t.Fatal(err)
		}
		for _, htmx := range []bool{false, true} {
			for _, method := range []string{http.MethodGet, http.MethodPost} {
				w := h.request(method, "/auth/reset/email?token="+url.QueryEscape(token.Token), url.Values{"password": {"NewPassword1!"}, "passwordMatch": {"NewPassword1!"}}, htmx)
				if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "Please request a new link") {
					t.Fatal("expired reset did not return a client error with recovery instructions")
				}
				h.assertNoTokenExposure(t, w, token.Token)
				h.assertUserUnchanged(t, u)
				h.assertRowCount(t, "usertoken", 1)
			}
		}
	})
}
