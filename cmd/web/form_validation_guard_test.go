package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPRejectedFormsHaveNoSideEffects(t *testing.T) {
	for _, htmx := range []bool{false, true} {
		t.Run(fmt.Sprintf("htmx=%t", htmx), func(t *testing.T) {
			runHTTPScenario(t, func(h *httpHarness) {
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
