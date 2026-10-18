package web_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/server"
)

// Use a real listener: ResponseRecorder does not enforce HTTP write deadlines.
func TestResetAcknowledgmentAfterSlowMail(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "delivered"
		if fail {
			name = "delivery failed"
		}
		t.Run(name, func(t *testing.T) {
			h := newHTTPHarness(t)
			u := h.seedUser(t, "slow@example.invalid")
			h.mail.beforeSend = func(ctx context.Context) {
				timer := time.NewTimer(50 * time.Millisecond)
				defer timer.Stop()
				select {
				case <-timer.C:
				case <-ctx.Done():
				}
			}
			if fail {
				h.mail.err = errors.New("delivery timed out")
			}
			httpServer, err := server.NewHTTPServer(config.HTTPConfig{WriteTimeout: 5 * time.Second}, h.handler)
			if err != nil {
				t.Fatal(err)
			}
			listener := httptest.NewUnstartedServer(h.handler)
			listener.Config = httpServer
			listener.Start()
			defer listener.Close()
			response, err := listener.Client().PostForm(listener.URL+"/auth/reset/request/email", url.Values{"email": {u.Email}})
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil || response.StatusCode != http.StatusOK || !strings.Contains(string(body), "Password reset request received.") {
				t.Fatalf("acknowledgment: status=%d error=%v body=%s", response.StatusCode, err, body)
			}
			if len(h.mail.snapshot()) != 1 {
				t.Fatal("mail operation did not finish before response")
			}
		})
	}
}
