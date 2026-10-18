package web_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/trentnix/hyperserver/auth/password"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
)

func registrationForm() url.Values {
	return url.Values{
		"email":    {"person@example.invalid"},
		"password": {"TestPassword1!"}, "passwordMatch": {"TestPassword1!"},
	}
}

func TestHTTPRegistration(t *testing.T) {
	for _, htmx := range []bool{false, true} {
		t.Run(fmt.Sprintf("htmx=%t", htmx), func(t *testing.T) {
			runSiteScenario(t, func(h *httpHarness) {
				if _, err := h.accounts.GetByEmail(t.Context(), "person@example.invalid"); !errors.Is(err, sql.ErrNoRows) {
					t.Fatalf("expected an empty database, got %v", err)
				}
				w := h.request(http.MethodPost, "/auth/register/email", registrationForm(), htmx)
				assertRedirectResponse(t, w, "/login", htmx)
				u, err := h.accounts.GetByEmail(t.Context(), "person@example.invalid")
				if err != nil {
					t.Fatal(err)
				}
				if u.Verified || !u.VerificationRequired || u.Password == "" || u.Password == "TestPassword1!" {
					t.Fatalf("unexpected persisted account state: verified=%v, verification required=%v, password stored correctly=%v", u.Verified, u.VerificationRequired, u.Password != "" && u.Password != "TestPassword1!")
				}
				if len(h.mail.snapshot()) != 1 {
					t.Fatalf("mail attempts = %d, want 1", len(h.mail.snapshot()))
				}
				message := h.mail.snapshot()[0]
				if message.To != u.Email || message.From != "test@example.invalid" || message.Subject != "Verify your account" || !strings.Contains(message.Body, "/auth/verify?token=") {
					t.Fatalf("unexpected verification email: %+v", message)
				}
				w = h.request(http.MethodGet, "/login", nil, false)
				if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "You have been successfully registered.") {
					t.Fatalf("registration notification missing from next request: status %d, body %s", w.Code, w.Body.String())
				}
			})
		})
	}
}

func TestHTTPRegistrationDisabled(t *testing.T) {
	runSiteScenario(t, func(h *httpHarness) {
		u := h.seedUser(t, "existing@example.invalid")
		u.Verified, u.VerificationRequired = false, true
		if err := h.accounts.Update(context.Background(), u); err != nil {
			t.Fatal(err)
		}
		u, err := h.accounts.GetByID(t.Context(), u.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, htmx := range []bool{false, true} {
			for _, path := range []string{"/register", "/auth/register", "/auth/register/email"} {
				for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
					w := h.request(method, path, registrationForm(), htmx)
					if w.Code != http.StatusForbidden || strings.Contains(w.Body.String(), `id="register-form"`) {
						t.Errorf("disabled registration %s %s: status %d", method, path, w.Code)
					}
					if method != http.MethodHead && strings.TrimSpace(w.Body.String()) != "Registration is not available." {
						t.Errorf("disabled registration %s %s did not explain the restriction", method, path)
					}
				}
			}
			for _, path := range []string{"/", "/login", "/auth/login", "/auth/login/email"} {
				w := h.request(http.MethodGet, path, nil, htmx)
				if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "/auth/register") || strings.Contains(w.Body.String(), `href="/register"`) {
					t.Errorf("page %s failed or offered disabled registration", path)
				}
			}
		}
		h.assertRowCount(t, "user", 1)
		h.assertUserUnchanged(t, u)

		h.request(http.MethodPost, "/auth/reset/request/email", url.Values{"email": {u.Email}}, true)
		if len(h.mail.snapshot()) != 1 {
			t.Fatal("disabling registration broke password recovery")
		}
		w := h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {u.Email}, "password": {"TestPassword1!"}}, true)
		if w.Header().Get("HX-Redirect") != "/auth/request/verify" {
			t.Fatal("disabling registration broke login")
		}
		for _, htmx := range []bool{false, true} {
			for _, path := range []string{"/register", "/auth/register", "/auth/register/email"} {
				w := h.request(http.MethodPost, path, registrationForm(), htmx)
				if w.Code != http.StatusForbidden || strings.TrimSpace(w.Body.String()) != "Registration is not available." {
					t.Errorf("authenticated request to %s did not explain disabled registration", path)
				}
			}
		}
		h.assertRowCount(t, "user", 1)
		h.request(http.MethodPost, "/auth/request/verify", nil, true)
		if len(h.mail.snapshot()) != 2 {
			t.Fatal("disabling registration broke verification resend")
		}
	}, func(cfg *config.Config) { cfg.Auth.RegistrationEnabled = false })
}

func TestHTTPRegistrationWithoutVerification(t *testing.T) {
	runSiteScenario(t, func(h *httpHarness) {
		h.mail.err = messaging.ErrMailUnavailable
		w := h.request(http.MethodPost, "/auth/register/email", registrationForm(), true)
		if w.Header().Get("HX-Redirect") != "/login" {
			t.Fatal("registration without verification failed")
		}
		u, err := h.accounts.GetByEmail(t.Context(), "person@example.invalid")
		if err != nil || u.NeedsVerification() || len(h.mail.snapshot()) != 0 {
			t.Fatal("optional verification blocked the account or attempted delivery")
		}
		w = h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {u.Email}, "password": {"TestPassword1!"}}, true)
		if w.Header().Get("HX-Redirect") != "/" {
			t.Fatal("new account could not log in without verification")
		}
	}, func(cfg *config.Config) { cfg.Auth.RegisterRequiresVerification = false })
}

func TestHTTPRegistrationCanRecoverFromMailFailure(t *testing.T) {
	runSiteScenario(t, func(h *httpHarness) {
		h.mail.err = errors.New("delivery temporarily unavailable")
		w := h.request(http.MethodPost, "/auth/register/email", registrationForm(), true)
		assertFormRejected(t, w, "Your account was created, but we could not send the verification email.")
		if len(h.mail.snapshot()) != 1 {
			t.Fatal("registration did not attempt delivery")
		}
		link := mailLink(t, h.mail.snapshot()[0], "/auth/verify")
		h.assertNoTokenExposure(t, w, link.Query().Get("token"))
		w = h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {"person@example.invalid"}, "password": {"TestPassword1!"}}, true)
		if w.Header().Get("HX-Redirect") != "/auth/request/verify" {
			t.Fatal("pending account could not log in to request verification")
		}
		h.mail.err = nil
		w = h.request(http.MethodPost, "/auth/request/verify", nil, true)
		if len(h.mail.snapshot()) != 2 {
			t.Fatal("verification resend did not deliver instructions")
		}
		link = mailLink(t, h.mail.snapshot()[1], "/auth/verify")
		h.assertNoTokenExposure(t, w, link.Query().Get("token"))
		h.request(http.MethodPost, "/auth/verify", link.Query(), false)
		u, err := h.accounts.GetByEmail(t.Context(), "person@example.invalid")
		if err != nil || !u.Verified {
			t.Fatal("account did not recover after successful resend")
		}
		h.assertRowCount(t, "user", 1)
	})
}

func TestHTTPRegistrationMailFailure(t *testing.T) {
	runSiteScenario(t, func(h *httpHarness) {
		for i, tc := range []struct {
			name string
			err  error
		}{
			{"delivery failure", errors.New("test delivery failed")},
			{"unavailable", messaging.ErrMailUnavailable},
		} {
			t.Run(tc.name, func(t *testing.T) {
				h.mail.err = tc.err
				form := registrationForm()
				form.Set("email", fmt.Sprintf("mail-failure-%d@example.invalid", i))
				w := h.request(http.MethodPost, "/auth/register/email", form, true)
				assertFormRejected(t, w, "Your account was created, but we could not send the verification email.")
				if len(h.mail.snapshot()) != i+1 {
					t.Fatalf("mail attempts = %d, want %d", len(h.mail.snapshot()), i+1)
				}
				u, err := h.accounts.GetByEmail(t.Context(), form.Get("email"))
				if err != nil {
					t.Fatalf("account should remain available after delivery failure: %v", err)
				}
				if u.Verified || !u.VerificationRequired {
					t.Error("delivery failure changed the account's verification requirements")
				}
				w = h.request(http.MethodGet, "/login", nil, false)
				if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "You have been successfully registered.") {
					t.Error("delivery failure produced a success notification or broke the next request")
				}
			})
		}
	})
}

func TestHTTPConcurrentRegistration(t *testing.T) {
	runSiteScenario(t, func(h *httpHarness) {
		// Queue SQLite writes on one connection so this tests uniqueness, not
		// lock-timeout behavior. Schema initialization starts from an empty DB.
		h.db.SetMaxOpenConns(1)
		passwords := []string{"FirstPassword1!", "SecondPassword2!"}
		for attempt := 0; attempt < 5; attempt++ {
			email := fmt.Sprintf("race-%d@example.invalid", attempt)
			responses := make([]*httptest.ResponseRecorder, len(passwords))
			start := make(chan struct{})
			var requests sync.WaitGroup
			for i, pw := range passwords {
				requests.Add(1)
				go func() {
					defer requests.Done()
					form := url.Values{"email": {email}, "password": {pw}, "passwordMatch": {pw}}
					r := httptest.NewRequest(http.MethodPost, "/auth/register/email", strings.NewReader(form.Encode()))
					r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					r.Header.Set("HX-Request", "true")
					w := httptest.NewRecorder()
					<-start
					h.handler.ServeHTTP(w, r)
					responses[i] = w
				}()
			}
			close(start)
			requests.Wait()

			winner, successes, duplicates := -1, 0, 0
			for i, w := range responses {
				if w.Code != http.StatusOK {
					t.Fatalf("registration status = %d, want 200", w.Code)
				}
				switch {
				case w.Header().Get("HX-Redirect") == "/login":
					winner = i
					successes++
				case strings.Contains(w.Body.String(), "The specified user is already registered"):
					duplicates++
				default:
					t.Fatalf("unexpected registration response: %s", w.Body.String())
				}
			}
			if successes != 1 || duplicates != 1 {
				t.Fatalf("concurrent registrations: successes=%d, duplicates=%d, want one of each", successes, duplicates)
			}
			u, err := h.accounts.GetByEmail(t.Context(), email)
			if err != nil {
				t.Fatal(err)
			}
			if !password.CheckPasswordHash(passwords[winner], u.Password) || password.CheckPasswordHash(passwords[1-winner], u.Password) {
				t.Fatal("losing registration overwrote the winning password")
			}
			if len(h.mail.snapshot()) != attempt+1 {
				t.Fatalf("mail attempts = %d, want %d", len(h.mail.snapshot()), attempt+1)
			}
			h.assertRowCount(t, "user", attempt+1)
		}
	})
}

func TestHTTPRegistrationRejectsExistingAccount(t *testing.T) {
	runSiteScenario(t, func(h *httpHarness) {
		u := h.seedUser(t, "person@example.invalid")
		form := registrationForm()
		form.Set("email", "PERSON@EXAMPLE.INVALID")
		form.Set("password", "DifferentPassword1!")
		form.Set("passwordMatch", "DifferentPassword1!")
		w := h.request(http.MethodPost, "/auth/register/email", form, true)
		assertFormRejected(t, w, "The specified user is already registered")
		h.assertUserUnchanged(t, u)
		h.assertRowCount(t, "user", 1)
	})
}

func TestHTTPRegistrationRejectsInvalidForm(t *testing.T) {
	for _, htmx := range []bool{false, true} {
		t.Run(fmt.Sprintf("htmx=%t", htmx), func(t *testing.T) {
			runSiteScenario(t, func(h *httpHarness) {
				// Confirm the account store is empty before submitting invalid forms.
				if _, err := h.accounts.GetByEmail(t.Context(), "person@example.invalid"); !errors.Is(err, sql.ErrNoRows) {
					t.Fatalf("expected an empty database, got %v", err)
				}
				for _, tc := range []struct {
					name, email, password, confirmation, message string
				}{
					{"mismatch", "person@example.invalid", "TestPassword1!", "DifferentPassword1!", "Passwords do not match."},
					{"weak password", "person@example.invalid", "short", "short", "password must contain at least 8 characters"},
					{"invalid email", "not-an-email", "TestPassword1!", "TestPassword1!", "Enter a valid email address."},
					{"domain slash", "person@foo/bar.com", "TestPassword1!", "TestPassword1!", "Enter a valid email address."},
					{"domain leading hyphen", "person@-example.com", "TestPassword1!", "TestPassword1!", "Enter a valid email address."},
					{"domain trailing hyphen", "person@example-.com", "TestPassword1!", "TestPassword1!", "Enter a valid email address."},
					{"display name", "Person <person@example.invalid>", "TestPassword1!", "TestPassword1!", "Enter a valid email address."},
					{"email line break", "person@example.invalid\r\nBcc: other@example.invalid", "TestPassword1!", "TestPassword1!", "Enter a valid email address."},
					{"missing email", "", "TestPassword1!", "TestPassword1!", "This field is required."},
					{"missing password", "person@example.invalid", "", "", "This field is required."},
					{"missing confirmation", "person@example.invalid", "TestPassword1!", "", "This field is required."},
				} {
					t.Run(tc.name, func(t *testing.T) {
						form := url.Values{"email": {tc.email}, "password": {tc.password}, "passwordMatch": {tc.confirmation}}
						w := h.request(http.MethodPost, "/auth/register/email", form, htmx)
						assertFormRejected(t, w, tc.message)
						h.assertRowCount(t, "user", 0)
						h.assertRowCount(t, "usertoken", 0)
						if len(h.mail.snapshot()) != 0 {
							t.Fatal("invalid registration attempted mail delivery")
						}
					})
				}
			})
		})
	}
}
