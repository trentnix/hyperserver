package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/components/messages"
	"github.com/trentnix/hyperserver/pkg/server"
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
			if _, err := loader(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil)); err == nil {
				t.Fatal("session-backed notifications accepted missing sessions")
			}
		}

		called := false
		app.ContentManager.LoadNotifications = func(http.ResponseWriter, *http.Request) ([]messages.Notification, error) {
			called = true
			return nil, nil
		}
		if err := configureRendering(app); err == nil {
			t.Fatal("duplicate setup accepted")
		}
		if _, err := app.ContentManager.LoadNotifications(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil)); err != nil || !called {
			t.Fatal("duplicate setup replaced application override")
		}
	}
}
