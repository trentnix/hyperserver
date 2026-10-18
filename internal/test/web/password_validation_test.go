package web_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/auth/password"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestHTTPPasswordBoundaries(t *testing.T) {
	for _, operation := range []string{"register", "reset", "change"} {
		for _, htmx := range []bool{false, true} {
			for _, tc := range []struct{ name, value, message string }{
				{"72 ASCII bytes", strings.Repeat("a", 69) + "a1!", ""},
				{"73 ASCII bytes", strings.Repeat("a", 70) + "a1!", "password must not exceed 72 bytes in UTF-8"},
				{"72 Unicode bytes", strings.Repeat("界", 23) + "a1!", ""},
				{"73 Unicode bytes", strings.Repeat("界", 23) + "aa1!", "password must not exceed 72 bytes in UTF-8"},
				{"seven Unicode characters", "a1!界界界界", "password must contain at least 8 characters"},
				{"eight Unicode characters", "a1!界界界界界", ""},
				{"invalid UTF-8", "Password1!\xff", "password must contain valid UTF-8 text"},
			} {
				t.Run(fmt.Sprintf("%s/htmx=%t/%s", operation, htmx, tc.name), func(t *testing.T) {
					runSiteScenario(t, func(h *httpHarness) {
						const email = "boundary@example.invalid"
						path, field := "/auth/register/email", "password"
						values := url.Values{"email": {email}, "password": {tc.value}, "passwordMatch": {tc.value}}
						var account *user.User
						var token *user.AuthResetToken
						if operation != "register" {
							account = h.seedUser(t, email)
						}
						if operation == "reset" {
							var err error
							token, err = user.NewAuthResetToken(account, []byte(h.app.Config.Auth.JwtKey), time.Hour)
							if err != nil {
								t.Fatal(err)
							}
							if err := h.accounts.InsertToken(t.Context(), token.Metadata()); err != nil {
								t.Fatal(err)
							}
							path = "/auth/reset/email?token=" + url.QueryEscape(token.Token)
						} else if operation == "change" {
							h.establishSession(t, account)
							path, field = "/auth/change/email", "newPassword"
							values = url.Values{"oldPassword": {"TestPassword1!"}, "newPassword": {tc.value}, "newPasswordMatch": {tc.value}}
						}

						w := h.request(http.MethodPost, path, values, htmx)
						if strings.Contains(w.Body.String()+h.logs.String(), tc.value) {
							t.Fatal("response or logs exposed the password")
						}
						if tc.message != "" {
							assertFormRejected(t, w, tc.message)
							if !strings.Contains(w.Body.String(), `name="`+field+`"`) || !strings.Contains(w.Body.String(), `aria-invalid="true"`) || !strings.Contains(w.Body.String(), `class="field-error"`) {
								t.Fatal("invalid password did not produce a field error")
							}
							if len(h.mail.snapshot()) != 0 {
								t.Fatal("invalid password sent mail")
							}
							if operation == "register" {
								h.assertRowCount(t, "user", 0)
								h.assertRowCount(t, "usertoken", 0)
							} else {
								h.assertUserUnchanged(t, account)
								if operation == "reset" {
									if _, err := h.accounts.GetToken(t.Context(), token.TokenHash, user.ResetTokenPurpose); err != nil {
										t.Fatalf("invalid password consumed the token: %v", err)
									}
								}
							}
							return
						}

						if operation == "change" {
							if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Your password has been changed.") {
								t.Fatalf("valid change rejected: %d %s", w.Code, w.Body.String())
							}
						} else {
							assertRedirectResponse(t, w, "/login", htmx)
						}
						stored, err := h.accounts.GetByEmail(t.Context(), email)
						if err != nil || !password.CheckPasswordHash(tc.value, stored.Password) {
							t.Fatalf("accepted password was not stored intact: %v", err)
						}
						if operation == "reset" {
							h.assertRowCount(t, "usertoken", 0)
						}
					})
				})
			}
		}
	}
}

func TestHTTPAuthenticationRejectsPasswordSuffixBeyondByteLimit(t *testing.T) {
	for _, htmx := range []bool{false, true} {
		t.Run(fmt.Sprintf("htmx=%t", htmx), func(t *testing.T) {
			runSiteScenario(t, func(h *httpHarness) {
				value := strings.Repeat("界", 23) + "a1!"
				u := h.seedUser(t, "login-boundary@example.invalid")
				hash, err := password.HashPassword(value)
				if err != nil {
					t.Fatal(err)
				}
				if err := h.accounts.ChangePassword(t.Context(), u, hash); err != nil {
					t.Fatal(err)
				}
				// Compare stored snapshots, not in-memory timestamps with clock metadata.
				u, err = h.accounts.GetByID(t.Context(), u.ID)
				if err != nil {
					t.Fatal(err)
				}
				values := url.Values{"email": {u.Email}, "password": {value + "x"}}
				w := h.request(http.MethodPost, "/auth/login/email", values, htmx)
				assertFormRejected(t, w, "Invalid login/password")
				h.assertRowCount(t, "session", 0)
				values.Set("password", value)
				w = h.request(http.MethodPost, "/auth/login/email", values, htmx)
				assertRedirectResponse(t, w, "/", htmx)

				values = url.Values{"oldPassword": {value + "x"}, "newPassword": {"NewPassword1!"}, "newPasswordMatch": {"NewPassword1!"}}
				w = h.request(http.MethodPost, "/auth/change/email", values, htmx)
				assertFormRejected(t, w, "The provided password is incorrect")
				h.assertUserUnchanged(t, u)
				values.Set("oldPassword", value)
				w = h.request(http.MethodPost, "/auth/change/email", values, htmx)
				if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Your password has been changed.") {
					t.Fatalf("exact current password rejected: %d %s", w.Code, w.Body.String())
				}
			})
		})
	}
}

func TestHTTPResetRejectsInvalidPassword(t *testing.T) {
	runSiteScenario(t, func(h *httpHarness) {
		u := h.seedUser(t, "person@example.invalid")
		token, err := user.NewAuthResetToken(u, []byte(h.app.Config.Auth.JwtKey), time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if err := h.accounts.InsertToken(t.Context(), token.Metadata()); err != nil {
			t.Fatal(err)
		}
		path := "/auth/reset/email?token=" + url.QueryEscape(token.Token)
		for _, tc := range []struct{ password, confirmation, message string }{
			{"NewPassword1!", "DifferentPassword1!", "Passwords do not match."},
			{"short", "short", "password must contain at least 8 characters"},
			{"", "", "This field is required."},
		} {
			t.Run(tc.message, func(t *testing.T) {
				w := h.request(http.MethodPost, path, url.Values{"password": {tc.password}, "passwordMatch": {tc.confirmation}}, true)
				assertFormRejected(t, w, tc.message)
				if !strings.Contains(w.Body.String(), path) {
					t.Error("validation response lost the reset URL and token")
				}
				h.assertUserUnchanged(t, u)
				if _, err := h.accounts.GetToken(t.Context(), token.TokenHash, "auth-reset"); err != nil {
					t.Fatalf("invalid form consumed the reset token: %v", err)
				}
			})
		}
		w := h.request(http.MethodPost, path, url.Values{"password": {"NewPassword1!"}, "passwordMatch": {"NewPassword1!"}}, true)
		if w.Code != http.StatusOK || w.Header().Get("HX-Redirect") != "/login" {
			t.Fatal("corrected reset form did not succeed")
		}
		updated, err := h.accounts.GetByID(t.Context(), u.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !password.CheckPasswordHash("NewPassword1!", updated.Password) {
			t.Error("corrected reset form did not update the password")
		}
		h.assertRowCount(t, "usertoken", 0)
	})
}

func TestHTTPChangeRejectsInvalidPassword(t *testing.T) {
	runSiteScenario(t, func(h *httpHarness) {
		u := h.seedUser(t, "person@example.invalid")
		w := h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {u.Email}, "password": {"TestPassword1!"}}, true)
		if w.Code != http.StatusOK || w.Header().Get("HX-Redirect") != "/" {
			t.Fatal("could not log in the test user")
		}
		for _, tc := range []struct{ old, password, confirmation, message string }{
			{"TestPassword1!", "NewPassword1!", "DifferentPassword1!", "Passwords do not match."},
			{"TestPassword1!", "short", "short", "password must contain at least 8 characters"},
			{"TestPassword1!", "TestPassword1!", "TestPassword1!", "New passwords must be different from the previous password."},
			{"", "NewPassword1!", "NewPassword1!", "This field is required."},
		} {
			t.Run(tc.message, func(t *testing.T) {
				form := url.Values{"oldPassword": {tc.old}, "newPassword": {tc.password}, "newPasswordMatch": {tc.confirmation}}
				w := h.request(http.MethodPost, "/auth/change/email", form, true)
				assertFormRejected(t, w, tc.message)
				if !strings.Contains(w.Body.String(), `hx-post="/auth/change/email"`) {
					t.Error("validation response lost the change-password URL")
				}
				h.assertUserUnchanged(t, u)
			})
		}
		form := url.Values{"oldPassword": {"TestPassword1!"}, "newPassword": {"NewPassword1!"}, "newPasswordMatch": {"NewPassword1!"}}
		w = h.request(http.MethodPost, "/auth/change/email", form, true)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Your password has been changed.") {
			t.Fatal("corrected password-change form did not succeed")
		}
		updated, err := h.accounts.GetByID(t.Context(), u.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !password.CheckPasswordHash("NewPassword1!", updated.Password) {
			t.Error("corrected password-change form did not update the password")
		}
	})
}
