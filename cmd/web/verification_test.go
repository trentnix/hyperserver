package main

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/pkg/services/messaging"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestHTTPVerificationSingleUseAndResend(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		h.request(http.MethodPost, "/auth/register/email", registrationForm(), true)
		first := mailLink(t, h.mail.snapshot()[0], "/auth/verify")
		h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {"person@example.invalid"}, "password": {"TestPassword1!"}}, true)
		h.request(http.MethodPost, "/auth/request/verify", nil, true)
		second := mailLink(t, h.mail.snapshot()[1], "/auth/verify")
		if first.String() == second.String() {
			t.Fatal("resend reused the first token")
		}
		for i, link := range []*url.URL{second, first} {
			w := h.request(http.MethodGet, link.RequestURI(), nil, true)
			if w.Header().Get("HX-Redirect") != "/" {
				t.Fatal("delivered verification link was rejected")
			}
			h.assertRowCount(t, "usertoken", 1-i)
			for _, htmx := range []bool{false, true} {
				w = h.request(http.MethodGet, link.RequestURI(), nil, htmx)
				if !strings.Contains(w.Body.String(), "Verification failed") || w.Header().Get("HX-Redirect") != "" {
					t.Fatal("replayed link was not rejected")
				}
				h.assertNoTokenExposure(t, w, link.Query().Get("token"))
			}
		}
	})
}

func TestHTTPVerificationStorageFailure(t *testing.T) {
	for _, failure := range []string{"insert", "update", "delete"} {
		t.Run(failure, func(t *testing.T) {
			runHTTPScenario(t, func(h *httpHarness) {
				if failure == "insert" {
					// Prepare the schema without registering an account or sending mail.
					_, _ = user.GetUserByEmail(h.app.Database, "person@example.invalid")
					if _, err := h.app.Database.Exec(`CREATE TRIGGER fail_verification BEFORE INSERT ON usertoken BEGIN SELECT RAISE(ABORT, 'failed insertion'); END`); err != nil {
						t.Fatal(err)
					}
					w := h.request(http.MethodPost, "/auth/register/email", registrationForm(), true)
					if !strings.Contains(w.Body.String(), "verification email delivery failed") || len(h.mail.snapshot()) != 0 {
						t.Fatal("token storage failure was not reported before mail delivery")
					}
					h.assertRowCount(t, "user", 1)
					h.assertRowCount(t, "usertoken", 0)
					if _, err := h.app.Database.Exec(`DROP TRIGGER fail_verification`); err != nil {
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
					if _, err := h.app.Database.Exec(statement); err != nil {
						t.Fatal(err)
					}
					before, err := user.GetUserByEmail(h.app.Database, "person@example.invalid")
					if err != nil {
						t.Fatal(err)
					}
					link := mailLink(t, h.mail.snapshot()[0], "/auth/verify")
					for _, htmx := range []bool{false, true} {
						w := h.request(http.MethodGet, link.RequestURI(), nil, htmx)
						if !strings.Contains(w.Body.String(), "Verification failed") || w.Header().Get("HX-Redirect") != "" {
							t.Fatal("storage failure returned verification success")
						}
						stored, err := user.GetUserByID(h.app.Database, before.ID)
						if err != nil || *stored != *before {
							t.Fatalf("failed verification changed account: %v", err)
						}
						if len(h.mail.snapshot()) != 1 {
							t.Fatal("verification redemption attempted mail delivery")
						}
						h.assertRowCount(t, "usertoken", 1)
						h.assertNoTokenExposure(t, w, link.Query().Get("token"))
					}
					if _, err := h.app.Database.Exec(`DROP TRIGGER fail_verification`); err != nil {
						t.Fatal(err)
					}
				}
				link := mailLink(t, h.mail.snapshot()[0], "/auth/verify")
				w := h.request(http.MethodGet, link.RequestURI(), nil, true)
				if w.Header().Get("HX-Redirect") != "/" {
					t.Fatal("verification did not recover after storage failure")
				}
				h.assertRowCount(t, "usertoken", 0)
			})
		})
	}
}

func TestHTTPVerificationRejectsPreviousEmailAddress(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		h.request(http.MethodPost, "/auth/register/email", registrationForm(), true)
		oldLink := mailLink(t, h.mail.snapshot()[0], "/auth/verify")
		u, err := user.GetUserByEmail(h.app.Database, "person@example.invalid")
		if err != nil {
			t.Fatal(err)
		}
		u.Email = "new@example.invalid"
		if err := u.Update(context.Background(), h.app.Database); err != nil {
			t.Fatal(err)
		}
		for _, htmx := range []bool{false, true} {
			w := h.request(http.MethodGet, oldLink.RequestURI(), nil, htmx)
			if !strings.Contains(w.Body.String(), "Verification failed") || w.Header().Get("HX-Redirect") != "" {
				t.Fatal("old address link verified the new address")
			}
		}
		stored, err := user.GetUserByID(h.app.Database, u.ID)
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
		if w := h.request(http.MethodGet, newLink.RequestURI(), nil, true); w.Header().Get("HX-Redirect") != "/" {
			t.Fatal("new address link failed")
		}
	})
}

func TestHTTPVerificationFailedResendPreservesDeliveredLink(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		h.request(http.MethodPost, "/auth/register/email", registrationForm(), true)
		link := mailLink(t, h.mail.snapshot()[0], "/auth/verify")
		h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {"person@example.invalid"}, "password": {"TestPassword1!"}}, true)
		h.mail.err = messaging.ErrMailUnavailable
		w := h.request(http.MethodPost, "/auth/request/verify", nil, true)
		if !strings.Contains(w.Body.String(), "Unable to send verification instructions") {
			t.Fatal("failed resend was not reported")
		}
		w = h.request(http.MethodGet, link.RequestURI(), nil, true)
		if w.Header().Get("HX-Redirect") != "/" {
			t.Fatal("failed resend invalidated the delivered link")
		}
	})
}
