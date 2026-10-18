package web_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/pkg/services/messaging"
)

func TestHTTPVerificationRecoveryNavigation(t *testing.T) {
	for _, htmx := range []bool{false, true} {
		t.Run(fmt.Sprintf("htmx=%t", htmx), func(t *testing.T) {
			runSiteScenario(t, func(h *httpHarness) {
				w := h.request(http.MethodGet, "/auth/request/verify", nil, htmx)
				if w.Code != http.StatusUnauthorized || len(h.mail.snapshot()) != 0 {
					t.Fatal("anonymous request exposed verification controls or sent mail")
				}

				h.mail.err = errors.New("private SMTP failure")
				w = h.request(http.MethodPost, "/auth/register/email", registrationForm(), htmx)
				body := w.Body.String()
				for _, text := range []string{"Your account was created", `href="/auth/login/email"`, "Protected pages remain unavailable"} {
					if !strings.Contains(body, text) {
						t.Fatalf("registration failure is missing %q", text)
					}
				}
				for _, markup := range []string{
					`id="registration-pending" class="verification-panel" aria-labelledby="registration-pending-title"`,
					`<h1 id="registration-pending-title">`,
					`class="verification-status verification-status--error" role="alert"`,
					`class="verification-actions"`,
				} {
					if !strings.Contains(body, markup) {
						t.Fatalf("pending registration is missing presentation markup %q", markup)
					}
				}
				if strings.Contains(body, `id="register-form"`) || strings.Contains(body, "private SMTP failure") {
					t.Fatal("registration failure offered duplicate registration or exposed SMTP details")
				}
				w = h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {"person@example.invalid"}, "password": {"TestPassword1!"}}, htmx)
				assertRedirectResponse(t, w, "/auth/request/verify", htmx)
				w = h.request(http.MethodGet, "/auth/request/verify", nil, htmx)
				if w.Code != http.StatusOK || len(h.mail.snapshot()) != 1 || !strings.Contains(w.Body.String(), "You are signed in") {
					t.Fatal("pending account did not reach instructions without sending mail")
				}
				for _, field := range []string{`method="post"`, `action="/auth/request/verify"`, `hx-post="/auth/request/verify"`, `hx-target="#verification-request"`, `class="verification-panel"`} {
					if !strings.Contains(w.Body.String(), field) {
						t.Fatalf("resend form is missing %s", field)
					}
				}
				w = h.request(http.MethodGet, "/auth/change/email", nil, htmx)
				if w.Code != http.StatusForbidden {
					t.Fatal("pending account gained access to a protected page")
				}

				w = h.request(http.MethodPost, "/auth/request/verify", nil, htmx)
				if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "Send verification email") || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("failed resend did not retain retry controls and an uncached error status")
				}
				if w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
					t.Fatal("failed resend must identify its HTML so HTMX can display it")
				}
				if !strings.Contains(w.Body.String(), `verification-status--error`) || !strings.Contains(w.Body.String(), `role="alert"`) {
					t.Fatal("failed resend did not display an accessible error inside the panel")
				}
				h.mail.err = nil
				w = h.request(http.MethodPost, "/auth/request/verify", nil, htmx)
				if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Verification email sent") {
					t.Fatal("successful resend did not explain the next step")
				}
				if !strings.Contains(w.Body.String(), `role="status"`) || strings.Contains(w.Body.String(), `verification-status--error`) {
					t.Fatal("successful resend retained the error presentation")
				}
				mail := h.mail.snapshot()
				link := mailLink(t, mail[len(mail)-1], "/auth/verify")
				h.assertNoTokenExposure(t, w, link.Query().Get("token"))
				w = h.request(http.MethodPost, "/auth/verify", link.Query(), htmx)
				assertRedirectResponse(t, w, "/", htmx)
				for _, method := range []string{http.MethodGet, http.MethodPost} {
					w = h.request(method, "/auth/request/verify", nil, htmx)
					if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "No verification needed") || strings.Contains(w.Body.String(), "Send verification email") || len(h.mail.snapshot()) != len(mail) {
						t.Fatal("verified account was offered verification or caused more mail")
					}
				}
				h.assertRowCount(t, "user", 1)
			})
		})
	}
}

func TestHTTPVerificationSingleUseAndResend(t *testing.T) {
	runSiteScenario(t, func(h *httpHarness) {
		h.request(http.MethodPost, "/auth/register/email", registrationForm(), true)
		first := mailLink(t, h.mail.snapshot()[0], "/auth/verify")
		h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {"person@example.invalid"}, "password": {"TestPassword1!"}}, true)
		h.request(http.MethodPost, "/auth/request/verify", nil, true)
		second := mailLink(t, h.mail.snapshot()[1], "/auth/verify")
		if first.String() == second.String() {
			t.Fatal("resend reused the first token")
		}
		for i, link := range []*url.URL{second, first} {
			w := h.request(http.MethodPost, "/auth/verify", link.Query(), i == 0)
			assertRedirectResponse(t, w, "/", i == 0)
			h.assertRowCount(t, "usertoken", 1-i)
			for _, htmx := range []bool{false, true} {
				w = h.request(http.MethodPost, "/auth/verify", link.Query(), htmx)
				if !strings.Contains(w.Body.String(), "Verification failed") || w.Header().Get("HX-Redirect") != "" {
					t.Fatal("replayed link was not rejected")
				}
				h.assertNoTokenExposure(t, w, link.Query().Get("token"))
			}
		}
	})
}

func TestHTTPVerificationRequiresConfirmation(t *testing.T) {
	runSiteScenario(t, func(h *httpHarness) {
		h.request(http.MethodPost, "/auth/register/email", registrationForm(), true)
		link := mailLink(t, h.mail.snapshot()[0], "/auth/verify")
		before, err := h.accounts.GetByEmail(t.Context(), registrationForm().Get("email"))
		if err != nil {
			t.Fatal(err)
		}
		for _, htmx := range []bool{false, true} {
			for _, method := range []string{http.MethodHead, http.MethodGet} {
				w := h.request(method, link.RequestURI(), nil, htmx)
				if w.Code != http.StatusOK || w.Header().Get("HX-Redirect") != "" || w.Header().Get("Location") != "" {
					t.Fatalf("%s did not display confirmation: status=%d", method, w.Code)
				}
				if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" {
					t.Fatal("confirmation response did not protect the token from caching or referrer disclosure")
				}
				if method == http.MethodGet {
					for _, field := range []string{`method="post"`, `action="/auth/verify"`, `hx-post="/auth/verify"`, `hx-target="#verify-account"`, `class="verification-panel"`, `name="token" value="` + link.Query().Get("token") + `"`} {
						if !strings.Contains(w.Body.String(), field) {
							t.Fatalf("confirmation form is missing %s", field)
						}
					}
				}
				stored, err := h.accounts.GetByID(t.Context(), before.ID)
				if err != nil || *stored != *before {
					t.Fatalf("%s changed the account: %v", method, err)
				}
				h.assertRowCount(t, "usertoken", 1)
			}
			// A POST must submit the token in its body, not merely visit the emailed URL.
			w := h.request(http.MethodPost, link.RequestURI(), nil, htmx)
			if !strings.Contains(w.Body.String(), "No verification token specified") {
				t.Fatal("query-only POST was accepted")
			}
			h.assertRowCount(t, "usertoken", 1)
		}
		w := h.request(http.MethodPost, "/auth/verify", link.Query(), true)
		if w.Header().Get("HX-Redirect") != "/" {
			t.Fatal("confirmation did not verify the account")
		}
		h.assertRowCount(t, "usertoken", 0)
	})
}

func TestHTTPVerificationConfirmationEscapesToken(t *testing.T) {
	runSiteScenario(t, func(h *httpHarness) {
		const token = `"><script>alert(1)</script>`
		for _, htmx := range []bool{false, true} {
			w := h.request(http.MethodGet, "/auth/verify?token="+url.QueryEscape(token), nil, htmx)
			if w.Code != http.StatusOK || strings.Contains(w.Body.String(), token) || !strings.Contains(w.Body.String(), `value="&#34;&gt;&lt;script&gt;alert(1)&lt;/script&gt;"`) {
				t.Fatal("confirmation did not escape the token")
			}
		}
	})
}

func TestHTTPVerificationStorageFailure(t *testing.T) {
	for _, failure := range []string{"insert", "update", "delete"} {
		t.Run(failure, func(t *testing.T) {
			runSiteScenario(t, func(h *httpHarness) {
				if failure == "insert" {
					// Prepare the schema without registering an account or sending mail.
					_, _ = h.accounts.GetByEmail(t.Context(), "person@example.invalid")
					if _, err := h.db.Exec(`CREATE TRIGGER fail_verification BEFORE INSERT ON usertoken BEGIN SELECT RAISE(ABORT, 'failed insertion'); END`); err != nil {
						t.Fatal(err)
					}
					w := h.request(http.MethodPost, "/auth/register/email", registrationForm(), true)
					if !strings.Contains(w.Body.String(), "we could not send the verification email") || len(h.mail.snapshot()) != 0 {
						t.Fatal("token storage failure was not reported before mail delivery")
					}
					h.assertRowCount(t, "user", 1)
					h.assertRowCount(t, "usertoken", 0)
					if _, err := h.db.Exec(`DROP TRIGGER fail_verification`); err != nil {
						t.Fatal(err)
					}
					h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {"person@example.invalid"}, "password": {"TestPassword1!"}}, true)
					h.request(http.MethodPost, "/auth/request/verify", nil, true)
				} else {
					h.request(http.MethodPost, "/auth/register/email", registrationForm(), true)
					statement := `CREATE TRIGGER fail_verification BEFORE UPDATE ON user BEGIN SELECT RAISE(ABORT, 'failed update'); END`
					if failure == "delete" {
						statement = `CREATE TRIGGER fail_verification BEFORE DELETE ON usertoken BEGIN SELECT RAISE(ABORT, 'failed deletion'); END`
					}
					if _, err := h.db.Exec(statement); err != nil {
						t.Fatal(err)
					}
					before, err := h.accounts.GetByEmail(t.Context(), "person@example.invalid")
					if err != nil {
						t.Fatal(err)
					}
					link := mailLink(t, h.mail.snapshot()[0], "/auth/verify")
					for _, htmx := range []bool{false, true} {
						w := h.request(http.MethodPost, "/auth/verify", link.Query(), htmx)
						if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "Verification failed") || w.Header().Get("HX-Redirect") != "" {
							t.Fatal("storage failure returned verification success")
						}
						stored, err := h.accounts.GetByID(t.Context(), before.ID)
						if err != nil || *stored != *before {
							t.Fatalf("failed verification changed account: %v", err)
						}
						if len(h.mail.snapshot()) != 1 {
							t.Fatal("verification redemption attempted mail delivery")
						}
						h.assertRowCount(t, "usertoken", 1)
						h.assertNoTokenExposure(t, w, link.Query().Get("token"))
					}
					if _, err := h.db.Exec(`DROP TRIGGER fail_verification`); err != nil {
						t.Fatal(err)
					}
				}
				link := mailLink(t, h.mail.snapshot()[0], "/auth/verify")
				w := h.request(http.MethodPost, "/auth/verify", link.Query(), true)
				if w.Header().Get("HX-Redirect") != "/" {
					t.Fatal("verification did not recover after storage failure")
				}
				h.assertRowCount(t, "usertoken", 0)
			})
		})
	}
}

func TestHTTPVerificationRejectsPreviousEmailAddress(t *testing.T) {
	runSiteScenario(t, func(h *httpHarness) {
		h.request(http.MethodPost, "/auth/register/email", registrationForm(), true)
		oldLink := mailLink(t, h.mail.snapshot()[0], "/auth/verify")
		u, err := h.accounts.GetByEmail(t.Context(), "person@example.invalid")
		if err != nil {
			t.Fatal(err)
		}
		u.Email = "new@example.invalid"
		if err := h.accounts.Update(context.Background(), u); err != nil {
			t.Fatal(err)
		}
		for _, htmx := range []bool{false, true} {
			w := h.request(http.MethodPost, "/auth/verify", oldLink.Query(), htmx)
			if !strings.Contains(w.Body.String(), "Verification failed") || w.Header().Get("HX-Redirect") != "" {
				t.Fatal("old address link verified the new address")
			}
		}
		stored, err := h.accounts.GetByID(t.Context(), u.ID)
		if err != nil || stored.Verified {
			t.Fatalf("old link verified account: %v", err)
		}
		h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {u.Email}, "password": {"TestPassword1!"}}, true)
		h.request(http.MethodPost, "/auth/request/verify", nil, true)
		message := h.mail.snapshot()[1]
		if message.To != u.Email {
			t.Fatal("resend used the old address")
		}
		newLink := mailLink(t, message, "/auth/verify")
		if w := h.request(http.MethodPost, "/auth/verify", newLink.Query(), true); w.Header().Get("HX-Redirect") != "/" {
			t.Fatal("new address link failed")
		}
	})
}

func TestHTTPVerificationFailedResendPreservesDeliveredLink(t *testing.T) {
	runSiteScenario(t, func(h *httpHarness) {
		h.request(http.MethodPost, "/auth/register/email", registrationForm(), true)
		link := mailLink(t, h.mail.snapshot()[0], "/auth/verify")
		h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {"person@example.invalid"}, "password": {"TestPassword1!"}}, true)
		h.mail.err = messaging.ErrMailUnavailable
		w := h.request(http.MethodPost, "/auth/request/verify", nil, true)
		if !strings.Contains(w.Body.String(), "Unable to send verification instructions") {
			t.Fatal("failed resend was not reported")
		}
		w = h.request(http.MethodPost, "/auth/verify", link.Query(), true)
		if w.Header().Get("HX-Redirect") != "/" {
			t.Fatal("failed resend invalidated the delivered link")
		}
	})
}

func TestHTTPVerificationUpdatesExistingAccount(t *testing.T) {
	runSiteScenario(t, func(h *httpHarness) {
		w := h.request(http.MethodPost, "/auth/register/email", registrationForm(), true)
		if w.Header().Get("HX-Redirect") != "/login" {
			t.Fatal("could not register the test account")
		}
		u, err := h.accounts.GetByEmail(t.Context(), registrationForm().Get("email"))
		if err != nil {
			t.Fatal(err)
		}
		link := mailLink(t, h.mail.snapshot()[0], "/auth/verify")
		w = h.request(http.MethodPost, "/auth/verify", link.Query(), true)
		if w.Code != http.StatusOK || w.Header().Get("HX-Redirect") != "/" {
			t.Fatal("verification did not succeed")
		}
		updated, err := h.accounts.GetByID(t.Context(), u.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !updated.Verified || updated.Password != u.Password || !updated.CreatedAt.Equal(u.CreatedAt) {
			t.Fatal("verification did not preserve the existing account")
		}
		h.assertRowCount(t, "user", 1)
	})
}

func TestHTTPVerificationResendEmailsInstructions(t *testing.T) {
	runSiteScenario(t, func(h *httpHarness) {
		u := h.seedUser(t, "verify@example.invalid")
		u.Verified, u.VerificationRequired = false, true
		if err := h.accounts.Update(context.Background(), u); err != nil {
			t.Fatal(err)
		}
		w := h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {u.Email}, "password": {"TestPassword1!"}}, true)
		if w.Header().Get("HX-Redirect") != "/auth/request/verify" {
			t.Fatal("login failed")
		}
		for i, htmx := range []bool{false, true} {
			w := h.request(http.MethodPost, "/auth/request/verify", url.Values{"email": {"attacker@example.invalid"}}, htmx)
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "check your email") {
				t.Error("verification request did not return an acknowledgment")
			}
			if len(h.mail.snapshot()) != i+1 {
				t.Fatal("verification instructions were not emailed")
			}
			message := h.mail.snapshot()[i]
			if message.To != u.Email || message.Subject != "Verify your account" {
				t.Error("verification email used the wrong recipient or subject")
			}
			link := mailLink(t, message, "/auth/verify")
			h.assertNoTokenExposure(t, w, link.Query().Get("token"))
		}
		link := mailLink(t, h.mail.snapshot()[0], "/auth/verify")
		w = h.request(http.MethodPost, "/auth/verify", link.Query(), true)
		h.assertNoTokenExposure(t, w, link.Query().Get("token"))
		verified, err := h.accounts.GetByID(t.Context(), u.ID)
		if err != nil || !verified.Verified {
			t.Fatal("emailed link did not verify the account")
		}
	})
}

func TestHTTPVerificationResendAuthorizationAndFailure(t *testing.T) {
	runSiteScenario(t, func(h *httpHarness) {
		w := h.request(http.MethodPost, "/auth/request/verify", nil, true)
		if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "Authentication required") || len(h.mail.snapshot()) != 0 {
			t.Fatal("anonymous resend was not denied")
		}
		u := h.seedUser(t, "resend@example.invalid")
		u.Verified, u.VerificationRequired = false, true
		if err := h.accounts.Update(context.Background(), u); err != nil {
			t.Fatal(err)
		}
		w = h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {u.Email}, "password": {"TestPassword1!"}}, true)
		if w.Header().Get("HX-Redirect") != "/auth/request/verify" {
			t.Fatal("login failed")
		}
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			w = h.request(method, "/auth/request/verify", nil, true)
			if w.Code != http.StatusOK || len(h.mail.snapshot()) != 0 {
				t.Error("verification page failed or sent mail without a POST")
			}
		}
		for _, htmx := range []bool{false, true} {
			for _, tc := range []struct {
				err         error
				includeBody bool
			}{
				{messaging.ErrMailUnavailable, false}, {context.DeadlineExceeded, false}, {nil, true},
			} {
				h.mail.err, h.mail.includeBodyInError = tc.err, tc.includeBody
				before := len(h.mail.snapshot())
				w = h.request(http.MethodPost, "/auth/request/verify", nil, htmx)
				if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "Unable to send verification instructions.") || len(h.mail.snapshot()) != before+1 {
					t.Fatal("verification delivery failure was not reported")
				}
				link := mailLink(t, h.mail.snapshot()[before], "/auth/verify")
				h.assertNoTokenExposure(t, w, link.Query().Get("token"))
				if !strings.Contains(h.logs.String(), "verification email delivery failed") {
					t.Error("verification failure was not logged")
				}
			}
		}
		stored, err := h.accounts.GetByID(t.Context(), u.ID)
		if err != nil || stored.Verified {
			t.Fatal("resend failure verified the account")
		}
		before := len(h.mail.snapshot())
		u.Verified = true
		if err := h.accounts.Update(context.Background(), u); err != nil {
			t.Fatal(err)
		}
		h.establishSession(t, u)
		w = h.request(http.MethodPost, "/auth/request/verify", nil, true)
		if w.Code != http.StatusOK || len(h.mail.snapshot()) != before {
			t.Error("verified account caused another email")
		}
		u.Verified, u.RegistrationAuthType = false, "missing-provider"
		if err := h.accounts.Update(context.Background(), u); err != nil {
			t.Fatal(err)
		}
		h.establishSession(t, u)
		w = h.request(http.MethodPost, "/auth/request/verify", nil, true)
		if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "Unable to send verification instructions.") || len(h.mail.snapshot()) != before {
			t.Error("missing verification provider was not handled")
		}
	})
}
