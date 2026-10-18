package web_test

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/auth/password"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestHTTPResetRequestEmailsInstructions(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		h.app.Config.App.Name = `<img src=x> & test`
		var nativeAction, nativeUserID string
		for i, htmx := range []bool{false, true} {
			u := h.seedUser(t, fmt.Sprintf("reset%d@example.invalid", i))
			form := url.Values{"email": {u.Email}, "to": {"attacker@example.invalid"}}
			w := h.request(http.MethodPost, "/auth/reset/request/email", form, htmx)

			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Password reset request received.") {
				t.Error("reset request did not return a generic acknowledgment")
			}
			if len(h.mail.snapshot()) != i+1 {
				t.Fatal("reset instructions were not emailed")
			}
			message := h.mail.snapshot()[i]
			if message.To != u.Email || message.Subject != "Reset your password" || strings.Contains(message.Body, "<img") || !strings.Contains(message.Body, "&lt;img") {
				t.Error("reset email used the wrong recipient, subject, or escaping")
			}
			link := mailLink(t, message, "/auth/reset/email")
			token := link.Query().Get("token")
			h.assertNoTokenExposure(t, w, token)
			unknown := h.request(http.MethodPost, "/auth/reset/request/email", url.Values{"email": {"unknown@example.invalid"}}, htmx)
			assertSameResetAcknowledgment(t, unknown, w)
			if len(h.mail.snapshot()) != i+1 {
				t.Error("unknown account caused mail delivery")
			}
			resetUser, err := user.ValidateResetToken(context.Background(), h.accounts, token, []byte(h.app.Config.Auth.JwtKey))
			if err != nil || resetUser.ID != u.ID {
				t.Fatal("emailed reset token does not belong to the account")
			}
			// Following the emailed link must not put its token in request logs.
			reset := h.request(http.MethodGet, link.RequestURI(), nil, htmx)
			if reset.Code != http.StatusOK || !strings.Contains(reset.Body.String(), "reset-password-form") {
				t.Error("emailed reset link did not open the reset form")
			}
			if !htmx {
				formTag := regexp.MustCompile(`<form id="reset-password-form"[^>]*>`).FindString(reset.Body.String())
				action := regexp.MustCompile(`\saction="([^"]+)"`).FindStringSubmatch(formTag)
				if !strings.Contains(formTag, `method="post"`) || len(action) != 2 {
					t.Fatal("reset form must submit passwords by POST without JavaScript")
				}
				actionURL, err := url.Parse(html.UnescapeString(action[1]))
				if err != nil || actionURL.Query().Get("token") != token {
					t.Fatal("reset form action lost the token")
				}
				nativeAction, nativeUserID = actionURL.RequestURI(), u.ID
			}
			if strings.Contains(h.logs.String(), token) {
				t.Error("following the reset link exposed its token in logs")
			}
		}
		h.request(http.MethodPost, nativeAction, url.Values{
			"password": {"ReplacementPassword1!"}, "passwordMatch": {"ReplacementPassword1!"},
		}, false)
		stored, err := h.accounts.GetByID(t.Context(), nativeUserID)
		if err != nil || !password.CheckPasswordHash("ReplacementPassword1!", stored.Password) {
			t.Fatal("ordinary reset form submission did not update the password")
		}
	})
}

func TestHTTPResetAcknowledgmentDoesNotRevealAccountState(t *testing.T) {
	for _, tc := range []struct {
		name, authType, failure string
		verified, wantMail      bool
	}{
		{name: "verified", authType: "email", verified: true, wantMail: true},
		{name: "pending verification", authType: "email", wantMail: true},
		{name: "other provider", authType: "other", verified: true},
		{name: "lookup failure", authType: "email", failure: "lookup"},
		{name: "token storage failure", authType: "email", failure: "token"},
	} {
		for _, htmx := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/htmx=%t", tc.name, htmx), func(t *testing.T) {
				runHTTPScenario(t, func(h *httpHarness) {
					u := h.seedUser(t, "existing@example.invalid")
					u.Verified, u.VerificationRequired = tc.verified, true
					u.RegistrationAuthType = tc.authType
					if err := h.accounts.Update(context.Background(), u); err != nil {
						t.Fatal(err)
					}
					u, err := h.accounts.GetByID(t.Context(), u.ID)
					if err != nil {
						t.Fatal(err)
					}
					unknownForm := url.Values{"email": {"unknown@example.invalid"}}
					baseline := h.request(http.MethodPost, "/auth/reset/request/email", unknownForm, htmx)
					if baseline.Code != http.StatusOK || !strings.Contains(baseline.Body.String(), "Password reset request received.") {
						t.Fatal("missing generic reset acknowledgment")
					}
					h.assertRowCount(t, "usertoken", 0)
					if len(h.mail.snapshot()) != 0 {
						t.Fatal("unknown account triggered delivery")
					}

					// Fail only account lookup or token persistence in this scenario's database.
					switch tc.failure {
					case "lookup":
						_, err = h.db.Exec("ALTER TABLE user RENAME TO unavailable_user")
					case "token":
						_, err = h.db.Exec(`CREATE TRIGGER fail_reset_token BEFORE INSERT ON usertoken
							BEGIN SELECT RAISE(ABORT, 'test token storage failure'); END`)
					}
					if err != nil {
						t.Fatal(err)
					}
					known := h.request(http.MethodPost, "/auth/reset/request/email", url.Values{"email": {u.Email}}, htmx)
					unknown := h.request(http.MethodPost, "/auth/reset/request/email", unknownForm, htmx)
					assertSameResetAcknowledgment(t, baseline, known)
					assertSameResetAcknowledgment(t, baseline, unknown)
					if tc.failure != "" && !strings.Contains(h.logs.String(), "password reset instructions could not be sent") {
						t.Fatal("storage failure was not logged")
					}

					wantAttempts := 0
					if tc.wantMail {
						wantAttempts = 1
					}
					if len(h.mail.snapshot()) != wantAttempts {
						t.Fatalf("mail attempts = %d, want %d", len(h.mail.snapshot()), wantAttempts)
					}
					for _, message := range h.mail.snapshot() {
						if message.To != u.Email {
							t.Fatal("reset email used the wrong recipient")
						}
						link := mailLink(t, message, "/auth/reset/email")
						h.assertNoTokenExposure(t, known, link.Query().Get("token"))
					}
					if tc.failure == "lookup" {
						if _, err := h.db.Exec("ALTER TABLE unavailable_user RENAME TO user"); err != nil {
							t.Fatal(err)
						}
					}
					h.assertRowCount(t, "user", 1)
					h.assertRowCount(t, "usertoken", wantAttempts)
					stored, err := h.accounts.GetByID(t.Context(), u.ID)
					if err != nil || *stored != *u {
						t.Fatal("reset request changed the account")
					}
				})
			})
		}
	}
}

func assertSameResetAcknowledgment(t *testing.T, want, got *httptest.ResponseRecorder) {
	t.Helper()
	wantHeaders, gotHeaders := want.Header().Clone(), got.Header().Clone()
	// The logger assigns a fresh random tracing ID to every request, independent of the account.
	for _, headers := range []http.Header{wantHeaders, gotHeaders} {
		if headers.Get("X-Request-ID") == "" {
			t.Fatal("missing request tracing ID")
		}
		headers.Del("X-Request-ID")
	}
	if got.Code != want.Code || got.Body.String() != want.Body.String() || !reflect.DeepEqual(gotHeaders, wantHeaders) {
		t.Fatal("reset acknowledgment differs in status, body, or headers")
	}
}

func TestHTTPResetMailFailureAcknowledgment(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		for _, htmx := range []bool{false, true} {
			for i, tc := range []struct {
				name        string
				err         error
				includeBody bool
			}{
				{name: "disabled delivery", err: messaging.ErrMailUnavailable},
				{name: "timeout", err: context.DeadlineExceeded},
				{name: "error includes message body", includeBody: true},
			} {
				t.Run(fmt.Sprintf("%s/htmx=%t", tc.name, htmx), func(t *testing.T) {
					u := h.seedUser(t, fmt.Sprintf("failure-%d-%t@example.invalid", i, htmx))
					h.mail.err, h.mail.includeBodyInError = tc.err, tc.includeBody
					before := len(h.mail.snapshot())
					w := h.request(http.MethodPost, "/auth/reset/request/email", url.Values{"email": {u.Email}}, htmx)
					if len(h.mail.snapshot()) != before+1 {
						t.Fatal("reset delivery was not attempted")
					}
					unknown := h.request(http.MethodPost, "/auth/reset/request/email", url.Values{"email": {"unknown@example.invalid"}}, htmx)
					if w.Code != http.StatusOK {
						t.Fatal("mail failure changed the response status")
					}
					assertSameResetAcknowledgment(t, unknown, w)
					link := mailLink(t, h.mail.snapshot()[before], "/auth/reset/email")
					h.assertNoTokenExposure(t, w, link.Query().Get("token"))
					if !strings.Contains(h.logs.String(), "password reset instructions could not be sent") {
						t.Fatal("delivery failure was not logged")
					}
					stored, err := h.accounts.GetByID(t.Context(), u.ID)
					if err != nil || *stored != *u {
						t.Fatal("failed reset request changed the account")
					}
				})
			}
		}
	})
}
