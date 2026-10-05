package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/middleware"
	"github.com/trentnix/hyperserver/pkg/services/session"
)

func TestHTTPAuthenticationRedirectSaveFailure(t *testing.T) {
	for _, htmx := range []bool{false, true} {
		name := "navigation"
		if htmx {
			name = "htmx"
		}
		t.Run(name, func(t *testing.T) {
			runHTTPScenario(t, func(h *httpHarness) {
				// Reads and initialization work. Only saving the redirect session fails.
				if _, err := h.app.Database.Exec(`CREATE TRIGGER reject_redirect BEFORE INSERT ON session BEGIN SELECT RAISE(ABORT, 'redirect write failed'); END`); err != nil {
					t.Fatal(err)
				}
				h.app.Web.Handle("GET /protected-save-check", middleware.RequireAuthentication(h.app.Database, h.app.ContentManager)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
					t.Error("anonymous request reached the protected handler")
				})))
				w := h.request(http.MethodGet, "/protected-save-check", nil, htmx)
				if w.Code != http.StatusInternalServerError || w.Body.String() != "Unable to save the login session\n" {
					t.Fatalf("save failure response = %d %q", w.Code, w.Body.String())
				}
				if w.Header().Get("HX-Redirect") != "" || w.Header().Get("Location") != "" || len(w.Result().Cookies()) != 0 {
					t.Fatal("failed save redirected or wrote cookies")
				}
				if !strings.Contains(h.logs.String(), "redirect write failed") {
					t.Fatal("save failure was not logged")
				}
				h.assertRowCount(t, "session", 0)

				if _, err := h.app.Database.Exec(`DROP TRIGGER reject_redirect`); err != nil {
					t.Fatal(err)
				}
				h.logs.mu.Lock()
				h.logs.lines = nil
				h.logs.mu.Unlock()
				w = h.request(http.MethodGet, "/protected-save-check", nil, htmx)
				assertRedirectResponse(t, w, "/login", htmx)
				if strings.Contains(h.logs.String(), "Request Error") {
					t.Fatalf("successful save logged an error: %s", h.logs.String())
				}
				h.assertRowCount(t, "session", 1)
			}, func(cfg *config.Config) {
				useSQLiteSessions(cfg)
				cfg.HTTP.Session.Stores["cookieStore"] = map[string]string{"enabled": "true"}
				cfg.HTTP.Session.Types["default"] = "cookieStore"
				cfg.HTTP.Session.Types[session.AuthSession] = "sqliteStore"
			})
		})
	}
}
