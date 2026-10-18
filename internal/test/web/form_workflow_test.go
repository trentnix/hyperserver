package web_test

import (
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/modules/site/models"
)

func assertContactForm(t *testing.T, w *httptest.ResponseRecorder, htmx bool, values url.Values) {
	t.Helper()
	body := w.Body.String()
	if w.Code != http.StatusOK || w.Header().Get("Location") != "" || w.Header().Get("HX-Redirect") != "" {
		t.Fatalf("contact response: status=%d headers=%v body=%s", w.Code, w.Header(), body)
	}
	if fullPage := strings.Contains(strings.ToLower(body), "<!doctype html>"); fullPage == htmx {
		t.Fatalf("contact response full page=%t, HTMX=%t", fullPage, htmx)
	}
	assertNoStore(t, w)
	if !strings.Contains(w.Header().Get("Vary"), "HX-Request") {
		t.Fatal("contact response does not vary on HX-Request")
	}
	form := regexp.MustCompile(`<form\s+id="contactForm"[^>]*>`).FindString(body)
	if !strings.Contains(body, `<label for="email">Email</label>`) {
		t.Fatal("email label is not associated with the email field")
	}
	for _, attribute := range []string{`method="post"`, `action="/contact"`, `hx-post="/contact"`, `hx-target="#contact"`, `hx-swap="outerHTML"`} {
		if !regexp.MustCompile(`\s` + regexp.QuoteMeta(attribute)).MatchString(form) {
			t.Fatalf("contact form lost %s: %s", attribute, form)
		}
	}
	for _, field := range []string{"name", "email", "message"} {
		pattern := `<input\s[^>]*name="` + field + `"[^>]*value="([^"]*)"`
		if field == "message" {
			pattern = `(?s)<textarea\s[^>]*name="message"[^>]*>(.*?)</textarea>`
		}
		match := regexp.MustCompile(pattern).FindStringSubmatch(body)
		if len(match) != 2 || html.UnescapeString(match[1]) != values.Get(field) {
			t.Fatalf("%s value=%v, want %q", field, match, values.Get(field))
		}
	}
	if strings.Contains(body, `<script>workflow</script>`) || strings.Contains(body, `value="" autofocus`) {
		t.Fatal("submitted markup was not escaped")
	}
}

// Follow the same form from first display through validation, a failed write,
// retry, and a fresh GET. Each mode uses the real middleware and SQLite repository.
func TestHTTPContactWorkflow(t *testing.T) {
	for _, htmx := range []bool{false, true} {
		t.Run(fmt.Sprintf("htmx=%t", htmx), func(t *testing.T) {
			runSiteScenario(t, func(h *httpHarness) {
				w := h.request(http.MethodGet, "/contact", nil, htmx)
				assertContactForm(t, w, htmx, nil)
				h.assertRowCount(t, "hyperserver_contact_submission", 0)

				fields := url.Values{
					"name":    {`" autofocus onfocus="alert(1)`},
					"email":   {"not-an-email"},
					"message": {"<script>workflow</script>\nKeep this text & punctuation."},
					"":        {"unexpected-empty-key-value"},
				}
				for _, email := range []string{"not-an-email", "test@a", "first.last@a", `"first.last@internal"@a`} {
					fields.Set("email", email)
					w = h.request(http.MethodPost, "/contact", fields, htmx)
					assertContactForm(t, w, htmx, fields)
					assertFormRejected(t, w, "Enter a valid email address.")
					if strings.Contains(w.Body.String(), "Your message has been submitted.") {
						t.Fatal("invalid submission reported success")
					}
					h.assertRowCount(t, "hyperserver_contact_submission", 0)
				}

				// Disable only this isolated test database's contact table.
				if _, err := h.db.Exec(`ALTER TABLE hyperserver_contact_submission RENAME TO unavailable_contact`); err != nil {
					t.Fatal(err)
				}
				fields.Set("email", "workflow@example.invalid")
				w = h.request(http.MethodPost, "/contact", fields, htmx)
				assertContactForm(t, w, htmx, fields)
				assertFormRejected(t, w, "Unable to save your message")
				for _, forbidden := range []string{"no such table", "hyperserver_contact_submission", "Your message has been submitted."} {
					if strings.Contains(w.Body.String(), forbidden) {
						t.Fatalf("storage failure response contains %q", forbidden)
					}
				}
				if !strings.Contains(h.logs.String(), "no such table") {
					t.Fatal("storage failure was not logged")
				}
				h.assertRowCount(t, "unavailable_contact", 0)
				if _, err := h.db.Exec(`ALTER TABLE unavailable_contact RENAME TO hyperserver_contact_submission`); err != nil {
					t.Fatal(err)
				}

				w = h.request(http.MethodPost, "/contact", fields, htmx)
				assertContactForm(t, w, htmx, nil)
				if !strings.Contains(w.Body.String(), "Your message has been submitted.") || strings.Contains(w.Body.String(), "unexpected-empty-key-value") || strings.Contains(w.Body.String(), "Unable to save your message") {
					t.Fatal("successful retry did not show success with cleared fields and errors")
				}
				h.assertRowCount(t, "hyperserver_contact_submission", 1)
				var stored models.ContactSubmission
				err := h.db.QueryRowContext(t.Context(), "SELECT id, name, email, message, created_at FROM hyperserver_contact_submission").
					Scan(&stored.Id, &stored.Name, &stored.Email, &stored.Message, &stored.CreatedAt)
				if err != nil {
					t.Fatal(err)
				}
				if stored.Name != fields.Get("name") || stored.Email != fields.Get("email") || stored.Message != fields.Get("message") || stored.Id == "" || stored.CreatedAt.IsZero() {
					t.Fatalf("stored submission=%+v", stored)
				}

				w = h.request(http.MethodGet, "/contact", nil, htmx)
				assertContactForm(t, w, htmx, nil)
				if strings.Contains(w.Body.String(), "Your message has been submitted.") {
					t.Fatal("success message leaked into a later request")
				}
				h.assertRowCount(t, "hyperserver_contact_submission", 1)
				if len(h.mail.snapshot()) != 0 {
					t.Fatal("contact storage attempted email delivery")
				}
			})
		})
	}
}

func TestHTTPRegistrationRedirectWorkflow(t *testing.T) {
	for _, htmx := range []bool{false, true} {
		t.Run(fmt.Sprintf("htmx=%t", htmx), func(t *testing.T) {
			runSiteScenario(t, func(h *httpHarness) {
				fields := registrationForm()
				fields.Set("passwordMatch", "DifferentPassword1!")
				w := h.request(http.MethodPost, "/auth/register/email", fields, htmx)
				assertFormRejected(t, w, "Passwords do not match.")
				assertBlankPasswordFields(t, w.Body.String(), fields.Get("password"), fields.Get("passwordMatch"))
				if !strings.Contains(w.Body.String(), `value="`+fields.Get("email")+`"`) {
					t.Fatal("registration lost the email on validation failure")
				}
				h.assertRowCount(t, "user", 0)

				fields.Set("passwordMatch", fields.Get("password"))
				w = h.request(http.MethodPost, "/auth/register/email", fields, htmx)
				assertRedirectResponse(t, w, "/login", htmx)
				h.assertRowCount(t, "user", 1)
				// Both a 303 and HX-Redirect navigate with a fresh, ordinary GET.
				w = h.request(http.MethodGet, "/login", nil, false)
				if w.Code != http.StatusOK || !strings.Contains(strings.ToLower(w.Body.String()), "<!doctype html>") {
					t.Fatal("registration redirect did not reach a full login page")
				}
				w = h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {fields.Get("email")}, "password": {fields.Get("password")}}, htmx)
				assertRedirectResponse(t, w, "/", htmx)
				w = h.request(http.MethodGet, "/", nil, false)
				if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Log out") {
					t.Fatalf("login redirect lost authentication: %d %s", w.Code, w.Body.String())
				}
				h.assertRowCount(t, "user", 1)
			}, func(cfg *config.Config) { cfg.Auth.RegisterRequiresVerification = false })
		})
	}
}

func TestHTTPRejectedFormsHaveNoSideEffects(t *testing.T) {
	for _, htmx := range []bool{false, true} {
		t.Run(fmt.Sprintf("htmx=%t", htmx), func(t *testing.T) {
			runSiteScenario(t, func(h *httpHarness) {
				for _, path := range []string{"/contact", "/auth/register/email", "/auth/login/email", "/auth/reset/request/email"} {
					for _, malformed := range []bool{false, true} {
						body, status := "email=not-an-email", http.StatusOK
						if malformed {
							body, status = "email=%zz", http.StatusBadRequest
						}
						r := httptest.NewRequest(http.MethodPost, h.baseURL+path, strings.NewReader(body))
						r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
						if htmx {
							r.Header.Set("HX-Request", "true")
						}
						w := httptest.NewRecorder()
						h.handler.ServeHTTP(w, r)
						if w.Code != status {
							t.Fatalf("%s malformed=%t: status=%d, body=%s", path, malformed, w.Code, w.Body.String())
						}
						if !malformed && !strings.Contains(w.Body.String(), "Enter a valid email address.") {
							t.Fatal("validation error was not displayed", path)
						}
						if w.Header().Get("Location") != "" || w.Header().Get("HX-Redirect") != "" {
							t.Fatal("invalid form redirected", path)
						}
						h.assertRowCount(t, "user", 0)
						h.assertRowCount(t, "usertoken", 0)
						h.assertRowCount(t, "hyperserver_contact_submission", 0)
						if len(h.mail.snapshot()) != 0 {
							t.Fatal("invalid form sent email", path)
						}
					}
				}
			})
		})
	}
}

func TestHTTPContactRejectsInvalidForm(t *testing.T) {
	runSiteScenario(t, func(h *httpHarness) {
		for _, htmx := range []bool{false, true} {
			for _, tc := range []struct{ field, value, message string }{
				{"name", "", "This field is required."},
				{"message", "", "This field is required."},
				{"email", "not-an-email", "Enter a valid email address."},
			} {
				t.Run(fmt.Sprintf("%s/htmx=%v", tc.field, htmx), func(t *testing.T) {
					form := url.Values{"name": {"Test person"}, "email": {"person@example.invalid"}, "message": {"Test message"}}
					form.Set(tc.field, tc.value)
					w := h.request(http.MethodPost, "/contact", form, htmx)
					assertFormRejected(t, w, tc.message)
					if strings.Contains(w.Body.String(), "Your message has been submitted.") {
						t.Error("invalid contact form reported success")
					}
					h.assertRowCount(t, "hyperserver_contact_submission", 0)
					if len(h.mail.snapshot()) != 0 {
						t.Error("invalid contact form attempted mail delivery")
					}
				})
			}
		}
	})
}

func TestHTTPContact(t *testing.T) {
	runSiteScenario(t, func(h *httpHarness) {
		for i, htmx := range []bool{false, true} {
			form := url.Values{"name": {"Test person"}, "email": {"person@example.invalid"}, "message": {"Test message"}}
			w := h.request(http.MethodPost, "/contact", form, htmx)
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Your message has been submitted.") {
				t.Fatalf("contact response (HTMX=%v): status %d, body %s", htmx, w.Code, w.Body.String())
			}
			if fullPage := strings.Contains(strings.ToLower(w.Body.String()), "<!doctype html>"); fullPage == htmx {
				t.Fatalf("contact response full page=%v, HTMX=%v", fullPage, htmx)
			}
			var count int
			if err := h.db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM hyperserver_contact_submission WHERE email = ? AND message = ?", "person@example.invalid", "Test message").Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != i+1 {
				t.Fatalf("saved contact submissions = %d, want %d", count, i+1)
			}
		}
		if len(h.mail.snapshot()) != 0 {
			t.Fatalf("contact storage unexpectedly sent mail: %+v", h.mail.snapshot())
		}
	})
}
