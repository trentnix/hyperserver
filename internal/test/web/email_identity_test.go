package web_test

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestHTTPEmailSpellingPreserved(t *testing.T) {
	for _, email := range []string{
		"Person@Example.INVALID",
		"élodie@bücher.invalid",
		`"person"@example.invalid`,
		`"first last"@example.invalid`,
		`"first..last"@example.invalid`,
		"person@" + strings.Repeat("界", 22) + ".invalid",
		"person@xn--koy" + strings.Repeat("a", 21) + ".invalid",
		"person@ab--cd.invalid",
		"person@r3---sn.invalid",
		"person@ab--cd.例え.invalid",
		"person@例え.ab--cd.invalid",
		"person@ab--cd.xn--r8jz45g.invalid",
	} {
		for _, htmx := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/htmx=%t", email, htmx), func(t *testing.T) {
				runSiteScenario(t, func(h *httpHarness) {
					registration := registrationForm()
					registration.Set("email", email)
					w := h.request(http.MethodPost, "/auth/register/email", registration, htmx)
					assertRedirectResponse(t, w, "/login", htmx)
					u, err := h.accounts.GetByEmail(t.Context(), email)
					if err != nil || u.Email != email {
						t.Fatalf("registration did not preserve the submitted email: %v", err)
					}
					mail := h.mail.snapshot()
					if len(mail) != 1 || mail[0].To != email {
						t.Fatal("verification mail did not use the stored account address")
					}

					w = h.request(http.MethodPost, "/auth/reset/request/email", url.Values{"email": {strings.ToUpper(email)}}, htmx)
					if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Password reset request received.") {
						t.Fatal("reset request rejected the submitted email")
					}
					mail = h.mail.snapshot()
					if len(mail) != 2 || mail[1].To != email || mail[1].Subject != "Reset your password" {
						t.Fatal("reset mail did not use the stored account address")
					}
					link := mailLink(t, mail[1], "/auth/reset/email")
					resetUser, err := user.ValidateResetToken(t.Context(), h.accounts, link.Query().Get("token"), []byte(h.app.Config.Auth.JwtKey))
					if err != nil || resetUser.ID != u.ID {
						t.Fatalf("reset token did not identify the account: %v", err)
					}

					w = h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {strings.ToUpper(email)}, "password": {"TestPassword1!"}}, htmx)
					assertRedirectResponse(t, w, "/auth/request/verify", htmx)
				})
			})
		}
	}
}

func TestHTTPInternationalizedEmailIdentity(t *testing.T) {
	emailInput := regexp.MustCompile(`<input\b[^>]*\bname="email"[^>]*>`)
	for _, htmx := range []bool{false, true} {
		t.Run(fmt.Sprintf("htmx=%t", htmx), func(t *testing.T) {
			runSiteScenario(t, func(h *httpHarness) {
				for _, path := range []string{"/auth/register/email", "/auth/login/email", "/auth/reset/request/email"} {
					w := h.request(http.MethodGet, path, nil, htmx)
					input := emailInput.FindString(w.Body.String())
					for _, attribute := range []string{`type="text"`, `inputmode="email"`, `autocomplete="username"`, `required`} {
						if w.Code != http.StatusOK || !strings.Contains(input, attribute) {
							t.Fatalf("%s email input missing %s: status %d, input %s", path, attribute, w.Code, input)
						}
					}
				}

				const original = "Élodie@bücher.invalid"
				const equivalent = "élodie@xn--bcher-kva.invalid"
				registration := registrationForm()
				registration.Set("email", original)
				w := h.request(http.MethodPost, "/auth/register/email", registration, htmx)
				assertRedirectResponse(t, w, "/login", htmx)
				account, err := h.accounts.GetByEmail(t.Context(), equivalent)
				if err != nil || account == nil || account.Email != original {
					t.Fatalf("equivalent lookup lost original address: %v", err)
				}

				registration.Set("email", equivalent)
				w = h.request(http.MethodPost, "/auth/register/email", registration, htmx)
				assertFormRejected(t, w, "The specified user is already registered")
				var count int
				if err := h.db.QueryRowContext(t.Context(), "SELECT count(*) FROM user").Scan(&count); err != nil || count != 1 {
					t.Fatalf("equivalent registration created another account: %d, %v", count, err)
				}
				mail := h.mail.snapshot()
				if len(mail) != 1 || mail[0].To != original {
					t.Fatal("verification mail lost the original address")
				}
				verification := mailLink(t, mail[0], "/auth/verify")
				w = h.request(http.MethodPost, "/auth/verify", verification.Query(), htmx)
				stored, err := h.accounts.GetByID(t.Context(), account.ID)
				if err != nil || stored == nil || !stored.Verified {
					t.Fatalf("internationalized account verification failed: %v, status %d", err, w.Code)
				}

				w = h.request(http.MethodPost, "/auth/reset/request/email", url.Values{"email": {equivalent}}, htmx)
				if w.Code != http.StatusOK {
					t.Fatalf("reset request failed: %d", w.Code)
				}
				mail = h.mail.snapshot()
				if len(mail) != 2 || mail[1].To != original {
					t.Fatal("reset lookup failed or delivery used the comparison address")
				}
				w = h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {equivalent}, "password": {"TestPassword1!"}}, htmx)
				assertRedirectResponse(t, w, "/", htmx)
			})
		})
	}
}

func TestHTTPLoginAndResetRequestRejectInvalidEmail(t *testing.T) {
	for _, tc := range []struct{ name, email string }{
		{"missing domain", "not-an-email"},
		{"slash", "person@foo/bar.com"},
		{"leading hyphen", "person@-example.com"},
		{"trailing hyphen", "person@example-.com"},
		{"empty label", "person@ab--cd..example"},
		{"invalid Punycode", "person@ab--cd.xn--a"},
		{"Unicode leading hyphen", "person@ab--cd.-é"},
		{"Unicode trailing hyphen", "person@ab--cd.é-"},
		{"Unicode double hyphen", "person@ab--cd.ab--é"},
		{"encoded leading hyphen", "person@ab--cd.xn----bga"},
		{"encoded trailing hyphen", "person@ab--cd.xn----9fa"},
		{"encoded double hyphen", "person@ab--cd.xn--ab---epa"},
		{"invalid bidi", "person@ab--cd.אa"},
		{"encoded label overflow", "person@" + strings.Repeat("界", 58) + ".example"},
		{"encoded domain overflow", "person@" + strings.Repeat(strings.Repeat("界", 57)+".", 3) + strings.Repeat("界", 56)},
	} {
		for _, htmx := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/htmx=%t", tc.name, htmx), func(t *testing.T) {
				runSiteScenario(t, func(h *httpHarness) {
					registration := registrationForm()
					registration.Set("email", tc.email)
					w := h.request(http.MethodPost, "/auth/register/email", registration, htmx)
					assertFormRejected(t, w, "Enter a valid email address.")
					h.assertRowCount(t, "user", 0)
					if len(h.mail.snapshot()) != 0 {
						t.Fatal("invalid registration attempted mail delivery")
					}

					// Seed a legacy account to prove validation blocks valid credentials
					// and reset-token creation before using the stored account.
					u := h.seedUser(t, tc.email)
					for _, path := range []string{"/auth/login/email", "/auth/reset/request/email"} {
						w := h.request(http.MethodPost, path, url.Values{"email": {u.Email}, "password": {"TestPassword1!"}}, htmx)
						assertFormRejected(t, w, "Enter a valid email address.")
						if !strings.Contains(w.Body.String(), `hx-post="`+path+`"`) {
							t.Error("validation response does not preserve the form submission URL")
						}
						h.assertUserUnchanged(t, u)
						h.assertRowCount(t, "usertoken", 0)
					}
					w = h.request(http.MethodGet, "/auth/change/email", nil, htmx)
					assertRedirectResponse(t, w, "/login", htmx)
				})
			})
		}
	}
}
