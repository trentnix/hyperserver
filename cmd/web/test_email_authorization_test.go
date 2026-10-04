package main

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestHTTPTestEmailAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		authenticated        bool
		verified             bool
		verificationRequired bool
		status               int
	}{
		{"anonymous", false, false, false, http.StatusForbidden},
		{"pending verification", true, false, true, http.StatusForbidden},
		{"verified", true, true, true, http.StatusOK},
		{"verification not required", true, false, false, http.StatusOK},
		{"revoked session", true, true, true, http.StatusForbidden},
		{"identity storage failure", true, true, true, http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runHTTPScenario(t, func(h *httpHarness) {
				if tc.authenticated {
					u := h.seedUser(t, "person@example.invalid")
					u.Verified, u.VerificationRequired = tc.verified, tc.verificationRequired
					if err := u.Update(context.Background(), h.app.Database); err != nil {
						t.Fatal(err)
					}
					h.establishSession(t, u)
					if tc.name == "revoked session" {
						if _, err := h.app.Database.Exec(`UPDATE user SET session_version = session_version + 1 WHERE id = ?`, u.ID); err != nil {
							t.Fatal(err)
						}
					}
					if tc.name == "identity storage failure" {
						if _, err := h.app.Database.Exec(`ALTER TABLE user RENAME TO unavailable_user`); err != nil {
							t.Fatal(err)
						}
					}
				}

				for _, htmx := range []bool{false, true} {
					for _, path := range []string{"/test-email?to=recipient@example.invalid", "/test-email"} {
						before := len(h.mail.snapshot())
						wantStatus, wantMail := tc.status, before
						if tc.status == http.StatusOK {
							if path == "/test-email" {
								wantStatus = http.StatusBadRequest
							} else {
								wantMail++
							}
						}
						w := h.request(http.MethodPost, path, nil, htmx)
						mail := h.mail.snapshot()
						if w.Code != wantStatus || len(mail) != wantMail {
							t.Fatalf("%s htmx=%t: status=%d mail=%d, want %d %d", path, htmx, w.Code, len(mail), wantStatus, wantMail)
						}
						if wantMail > before && mail[before].To != "recipient@example.invalid" {
							t.Fatalf("wrong mail recipient: %q", mail[before].To)
						}
						if strings.Contains(w.Body.String(), "unavailable_user") || len(w.Result().Cookies()) != 0 || w.Header().Get("Location") != "" || w.Header().Get("HX-Redirect") != "" {
							t.Fatalf("unexpected response: %v %s", w.Header(), w.Body.String())
						}
					}
				}
			})
		})
	}
}
