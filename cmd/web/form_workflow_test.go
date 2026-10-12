package main

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
			runHTTPScenario(t, func(h *httpHarness) {
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
				if _, err := h.app.Database.Exec(`ALTER TABLE hyperserver_contact_submission RENAME TO unavailable_contact`); err != nil {
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
				if _, err := h.app.Database.Exec(`ALTER TABLE unavailable_contact RENAME TO hyperserver_contact_submission`); err != nil {
					t.Fatal(err)
				}

				w = h.request(http.MethodPost, "/contact", fields, htmx)
				assertContactForm(t, w, htmx, nil)
				if !strings.Contains(w.Body.String(), "Your message has been submitted.") || strings.Contains(w.Body.String(), "unexpected-empty-key-value") || strings.Contains(w.Body.String(), "Unable to save your message") {
					t.Fatal("successful retry did not show success with cleared fields and errors")
				}
				var stored []models.ContactSubmission
				if err := h.app.Database.Select(&stored, "SELECT * FROM hyperserver_contact_submission"); err != nil {
					t.Fatal(err)
				}
				if len(stored) != 1 || stored[0].Name != fields.Get("name") || stored[0].Email != fields.Get("email") || stored[0].Message != fields.Get("message") || stored[0].Id == "" || stored[0].CreatedAt.IsZero() {
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
			runHTTPScenario(t, func(h *httpHarness) {
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
