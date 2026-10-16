package main

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestAuthenticationAudit(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		email := "audit-private@example.invalid"
		password := "Audit-Secret-Password1!"
		h.request(http.MethodPost, "/auth/register/email", url.Values{"email": {email}, "password": {password}, "passwordMatch": {password}}, false)
		h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {email}, "password": {"incorrect"}}, false)
		h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {email}, "password": {password}}, false)
		h.request(http.MethodPost, "/auth/logout", url.Values{}, false)
		logs := h.logs.String()
		for _, event := range []string{"event=registration outcome=succeeded", "event=login outcome=rejected", "event=login outcome=succeeded", "event=logout outcome=succeeded"} {
			if !strings.Contains(logs, event) {
				t.Fatalf("missing %s: %s", event, logs)
			}
		}
		if strings.Contains(logs, email) || strings.Contains(logs, password) {
			t.Fatal("audit exposed account credentials")
		}
		for line := range strings.SplitSeq(logs, "\n") {
			if strings.Contains(line, "event=") && !strings.Contains(line, "requestID=") {
				t.Fatal("audit lost correlation")
			}
		}
	})
}
