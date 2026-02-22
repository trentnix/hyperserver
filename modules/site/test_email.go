package module_site

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/trentnix/hyperserver/pkg/util"
)

const (
	defaultTestEmailSubject = "HyperServer test email"
	defaultTestEmailBody    = "<p>This is a test email from HyperServer.</p>"
)

// TestEmail sends a test email using SMTP settings from config.yaml.
// Example: /test-email?to=user@example.com&subject=Hi&body=<p>Hello</p>
func (m *SiteModule) TestEmail(w http.ResponseWriter, r *http.Request) {
	to := strings.TrimSpace(r.URL.Query().Get("to"))
	if to == "" {
		util.HttpError(w, r, "missing required query parameter: to", nil, http.StatusBadRequest)
		return
	}

	if m.mailClient == nil {
		util.HttpError(w, r, "mail client is not configured", nil, http.StatusInternalServerError)
		return
	}

	subject := strings.TrimSpace(r.URL.Query().Get("subject"))
	if subject == "" {
		subject = defaultTestEmailSubject
	}

	body := r.URL.Query().Get("body")
	if strings.TrimSpace(body) == "" {
		body = defaultTestEmailBody
	}

	err := m.mailClient.Compose().
		To(to).
		Subject(subject).
		Body(body).
		Send(r.Context())
	if err != nil {
		util.HttpError(w, r, "failed to send test email", err, http.StatusInternalServerError)
		return
	}

	util.HttpMessage(w, r, fmt.Sprintf("test email sent to %s", to))
}
