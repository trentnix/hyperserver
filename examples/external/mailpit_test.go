package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Run explicitly against this example's compose.yaml. Ordinary tests use captured
// mail and never need Docker. This test neither deletes mail nor modifies real accounts.
func TestMailpitRegistration(t *testing.T) {
	if os.Getenv("EXAMPLE_MAILPIT_TEST") != "1" {
		t.Skip("start Mailpit and set EXAMPLE_MAILPIT_TEST=1")
	}
	s := verificationSettings(t, nil)
	s.smtpPort = 11025
	h := newVerificationHarness(t, s, false)
	email := "test-" + uuid.NewString() + "@example.invalid"
	w := h.request(t, "POST", "/auth/register/email", registrationValues(email))
	h.redirect(t, w, "/auth/login/email")

	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	read := func(path string, target any) {
		t.Helper()
		request, err := http.NewRequestWithContext(t.Context(), "GET", "http://127.0.0.1:18026"+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatalf("Mailpit API status = %d", response.StatusCode)
		}
		if err := json.NewDecoder(io.LimitReader(response.Body, 2*1024*1024)).Decode(target); err != nil {
			t.Fatal(err)
		}
	}
	var messageID string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var inbox struct {
			Messages []struct {
				ID string
				To []struct{ Address string }
			}
		}
		read("/api/v1/messages", &inbox)
		for _, message := range inbox.Messages {
			for _, recipient := range message.To {
				if recipient.Address == email {
					messageID = message.ID
				}
			}
		}
		if messageID != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if messageID == "" {
		t.Fatal("SMTP accepted registration mail, but it was not found in Mailpit")
	}
	var message struct{ HTML string }
	read(fmt.Sprintf("/api/v1/message/%s", messageID), &message)
	link := verificationLink(t, message.HTML)
	if link.Host != "127.0.0.1:8083" {
		t.Fatal("unexpected verification origin")
	}
	w = h.request(t, "POST", "/auth/login/email", registrationValues(email))
	h.redirect(t, w, "/auth/request/verify")
	w = h.request(t, "POST", "/auth/verify", link.Query())
	h.redirect(t, w, "/notes")
	u, err := h.app.accounts.GetByEmail(t.Context(), email)
	if err != nil || u.NeedsVerification() {
		t.Fatal("Mailpit link did not verify the registered account", err)
	}
}
