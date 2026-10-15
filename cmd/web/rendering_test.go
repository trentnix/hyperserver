package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/htmx"
	"github.com/trentnix/hyperserver/pkg/components/messages"
	"github.com/trentnix/hyperserver/pkg/server"
	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/flash"
)

func TestApplicationSelectsNotifications(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		app, err := server.NewApplicationServer(config.Config{App: config.AppConfig{RenderNotifications: enabled}})
		if err != nil {
			t.Fatal(err)
		}
		if err := configureRendering(app); err != nil {
			t.Fatal(err)
		}
		loader := app.ContentManager.LoadNotifications
		if (loader != nil) != enabled {
			t.Fatalf("enabled=%t loader configured=%t", enabled, loader != nil)
		}
		if enabled {
			if _, _, err := loader(httptest.NewRequest(http.MethodGet, "/", nil)); err == nil {
				t.Fatal("session-backed notifications accepted missing sessions")
			}
		}

		called := false
		app.ContentManager.LoadNotifications = func(*http.Request) ([]messages.Notification, func(http.ResponseWriter) error, error) {
			called = true
			return nil, nil, nil
		}
		if err := configureRendering(app); err == nil {
			t.Fatal("duplicate setup accepted")
		}
		if _, _, err := app.ContentManager.LoadNotifications(httptest.NewRequest(http.MethodGet, "/", nil)); err != nil || !called {
			t.Fatal("duplicate setup replaced application override")
		}
	}
}

func TestHTTPRefreshPreservesNotifications(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		h.app.Web.HandleFunc("POST /test/queue-refresh-notice", func(w http.ResponseWriter, r *http.Request) {
			if err := flash.AddSuccessNotification(w, r, "Retained refresh notification"); err != nil {
				t.Fatal(err)
			}
		})
		h.app.Web.HandleFunc("GET /test/refresh", func(w http.ResponseWriter, r *http.Request) {
			cm := contentservice.NewContentManager()
			cm.LoadNotifications = flash.LoadNotifications
			page := content.NewManagedContent(r, cm)
			page.HTMX.Response = &htmx.Response{Refresh: true}
			page.AddContent(fstest.MapFS{"page.html": {Data: []byte(`{{range .Notifications}}{{.Message}}{{end}}`)}}, "page.html")
			if err := page.Render(w, r); err != nil {
				t.Fatal(err)
			}
		})
		h.request(http.MethodPost, "/test/queue-refresh-notice", nil, false)
		w := h.request(http.MethodGet, "/test/refresh", nil, true)
		if w.Code != http.StatusOK || w.Header().Get(htmx.HeaderRefresh) != "true" || len(w.Result().Cookies()) != 0 {
			t.Fatalf("refresh response: %d %v", w.Code, w.Header())
		}
		next := h.request(http.MethodGet, "/", nil, false)
		if next.Code != http.StatusOK || !strings.Contains(next.Body.String(), "Retained refresh notification") {
			t.Fatal("reload lost the notification")
		}
		if again := h.request(http.MethodGet, "/", nil, false); strings.Contains(again.Body.String(), "Retained refresh notification") {
			t.Fatal("displayed notification remained queued")
		}
	})
}
