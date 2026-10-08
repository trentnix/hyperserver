package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/components/messages"
)

func TestHTTPNotificationRemovalStorageFailure(t *testing.T) {
	for _, operation := range []string{"UPDATE", "DELETE"} {
		t.Run(operation, func(t *testing.T) {
			runHTTPScenario(t, func(h *httpHarness) {
				h.app.Web.HandleFunc("GET /test/seed-notification", func(w http.ResponseWriter, r *http.Request) {
					if err := messages.AddSuccessNotification(w, r, "Retained notification"); err != nil {
						t.Fatal(err)
					}
					if operation == "UPDATE" {
						if err := messages.AddMessage(w, r, "Other category", messages.AuthMessages); err != nil {
							t.Fatal(err)
						}
					}
				})
				h.request(http.MethodGet, "/test/seed-notification", nil, false)
				if _, err := h.app.Database.Exec("CREATE TRIGGER reject_message_removal BEFORE " + operation + " ON session BEGIN SELECT RAISE(ABORT, 'private message storage failure'); END"); err != nil {
					t.Fatal(err)
				}
				for _, htmx := range []bool{false, true} {
					w := h.request(http.MethodGet, "/", nil, htmx)
					if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "Retained notification") || strings.Contains(w.Body.String(), "private message storage failure") {
						t.Fatalf("storage failure: status=%d, body=%q", w.Code, w.Body.String())
					}
					if len(w.Result().Cookies()) != 0 {
						t.Fatal("failed removal changed cookies")
					}
				}
				if !strings.Contains(h.logs.String(), "private message storage failure") {
					t.Fatal("storage failure was not logged")
				}
				if _, err := h.app.Database.Exec("DROP TRIGGER reject_message_removal"); err != nil {
					t.Fatal(err)
				}
				w := h.request(http.MethodGet, "/", nil, false)
				if w.Code != http.StatusOK || strings.Count(w.Body.String(), "Retained notification") != 1 {
					t.Fatalf("retry did not display notification once: status=%d, body=%q", w.Code, w.Body.String())
				}
				w = h.request(http.MethodGet, "/", nil, false)
				if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "Retained notification") {
					t.Fatal("notification replayed after successful removal")
				}
			}, func(cfg *config.Config) { cfg.HTTP.Session.Types["hs-message-session"] = "sqliteStore" })
		})
	}
}
