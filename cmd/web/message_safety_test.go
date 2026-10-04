package main

import (
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"testing"

	email "github.com/trentnix/hyperserver/auth/modules/email"
	site "github.com/trentnix/hyperserver/modules/site"
	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/util"
)

func TestHTTPErrorDetailsStayInLogs(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		h.establishSession(t, h.seedUser(t, "mail-error@example.invalid"))
		h.mail.err = errors.New("private SMTP failure details")
		h.app.Web.HandleFunc("GET /test/form-render-error", func(w http.ResponseWriter, r *http.Request) {
			// No templates: exercise HandleFormError's rendering-failure response.
			form.HandleFormError(w, r, content.NewContent(r), &email.LoginForm{}, "Unable to display the form", errors.New("private form failure details"))
		})

		for _, htmx := range []bool{false, true} {
			for _, tc := range []struct{ method, path, message, logged string }{
				{http.MethodPost, "/test-email?to=recipient@example.invalid", "failed to send test email", "private SMTP failure details"},
				{http.MethodGet, "/test/form-render-error", "Unable to display the form", "private form failure details"},
			} {
				w := h.request(tc.method, tc.path, nil, htmx)
				if w.Code != http.StatusInternalServerError || w.Body.String() != tc.message+"\n" {
					t.Fatalf("%s: status=%d body=%q", tc.path, w.Code, w.Body.String())
				}
				if w.Header().Get("Content-Type") != "text/plain; charset=utf-8" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
					t.Fatalf("error response is not plain text: %v", w.Header())
				}
				requestID := w.Header().Get("X-Request-ID")
				logged := false
				for _, line := range strings.Split(h.logs.String(), "\n") {
					if requestID != "" && strings.Contains(line, requestID) && strings.Contains(line, tc.logged) {
						logged = true
					}
				}
				if !logged {
					t.Fatal("internal error or request correlation missing from logs")
				}
			}
		}
		if !strings.Contains(h.logs.String(), "No templates have been set") {
			t.Fatal("form rendering error was not logged")
		}
	})
}

func TestHTTPMessageEscaping(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		h.app.Web.HandleFunc("GET /test/site-message", func(w http.ResponseWriter, r *http.Request) {
			h.app.ContentManager.HandleMessage(w, r, r.URL.Query().Get("message"))
		})
		for _, htmx := range []bool{false, true} {
			for _, message := range []string{`<script>alert("message")</script>`, `</div><img src=x onerror="alert(1)">`, `Quotes " ' & Unicode café`, `&lt;script&gt;`, ""} {
				w := h.request(http.MethodGet, "/test/site-message?message="+url.QueryEscape(message), nil, htmx)
				want := template.HTMLEscapeString(message)
				if message == "" {
					want = "(no message)"
				}
				if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), want) {
					t.Fatalf("message %q htmx=%t: status=%d body=%s", message, htmx, w.Code, w.Body.String())
				}
				if message != "" && message != want && strings.Contains(w.Body.String(), message) {
					t.Fatalf("unescaped message %q", message)
				}
			}
		}
	})
}

func TestHTTPFormMessageEscaping(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		for _, tc := range []struct {
			name, path, partial string
			newForm             func() form.FormComponent
		}{
			{
				"login", "auth/modules/email/templates/html/partials/login-form.html", "auth.partial.email.login.form",
				func() form.FormComponent { return &email.LoginForm{} },
			},
			{
				"register", "auth/modules/email/templates/html/partials/register-form.html", "auth.partial.email.register.form",
				func() form.FormComponent { return &email.RegisterForm{} },
			},
			{
				"reset-request", "auth/modules/email/templates/html/partials/reset-request.html", "auth.partial.email.reset.request.form",
				func() form.FormComponent { return &email.ResetPasswordRequestForm{} },
			},
			{
				"reset-password", "auth/modules/email/templates/html/partials/reset-password.html", "auth.partial.email.reset.password.form",
				func() form.FormComponent { return &email.ResetPasswordForm{} },
			},
			{
				"change-password", "auth/modules/email/templates/html/partials/change-password.html", "auth.partial.email.change.password.form",
				func() form.FormComponent { return &email.ChangePasswordForm{} },
			},
			{
				"contact", "modules/site/templates/html/partials/contact-form.html", "site.partial.contact.form",
				func() form.FormComponent { return &site.ContactForm{} },
			},
		} {
			h.app.Web.HandleFunc("GET /test/form-messages/"+tc.name, func(w http.ResponseWriter, r *http.Request) {
				f := tc.newForm()
				message := r.URL.Query().Get("message")
				f.AddErrorMessage("error: " + message)
				f.AddSuccessMessage("success: " + message)
				f.AddMessage("info: " + message)
				c := content.NewManagedContent(r, h.app.ContentManager)
				c.PartialName, c.Data = tc.partial, f
				c.AddContent(content.TemplatePath(tc.path))
				if err := c.Render(w, r); err != nil {
					util.HttpError(w, r, "Unable to display test form", err, http.StatusInternalServerError)
				}
			})
			for _, htmx := range []bool{false, true} {
				for _, message := range []string{`<script>alert("form")</script>`, `</li><img src=x onerror="alert(1)">`, `Text & "quotes" ' café`, `&lt;b&gt;`} {
					w := h.request(http.MethodGet, "/test/form-messages/"+tc.name+"?message="+url.QueryEscape(message), nil, htmx)
					if w.Code != http.StatusOK || strings.Contains(w.Body.String(), message) {
						t.Fatalf("%s htmx=%t: status=%d body=%s", tc.name, htmx, w.Code, w.Body.String())
					}
					for _, kind := range []string{"error", "success", "info"} {
						if tc.name == "contact" && kind == "info" {
							continue // The contact form has no informational-message section.
						}
						want := "<li>" + kind + ": " + template.HTMLEscapeString(message) + "</li>"
						if !strings.Contains(w.Body.String(), want) {
							t.Fatalf("%s htmx=%t: missing escaped %s message %q", tc.name, htmx, kind, want)
						}
					}
				}
			}
		}
	})
}
