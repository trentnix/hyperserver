package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/components/messages"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
)

type testModule struct {
	bind  func(*routing.Routes)
	close func(context.Context) error
}

func (*testModule) Init(context.Context, *server.ApplicationServer) error { return nil }
func (m *testModule) Routes(routes *routing.Routes) error {
	if m.bind != nil {
		m.bind(routes)
	}
	return routes.Err()
}
func (m *testModule) Shutdown(ctx context.Context) error {
	if m.close != nil {
		return m.close(ctx)
	}
	return nil
}

func TestReferenceRenderingPreservesNotificationLoader(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		app, err := server.NewApplicationServer(config.Config{App: config.AppConfig{RenderNotifications: enabled}})
		if err != nil {
			t.Fatal(err)
		}
		called := false
		app.ContentManager.LoadNotifications = func(*http.Request) ([]messages.Notification, func(http.ResponseWriter) error, error) {
			called = true
			return nil, nil, nil
		}
		if err := configureRendering(app); err != nil {
			t.Fatal(err)
		}
		if _, _, err := app.ContentManager.LoadNotifications(httptest.NewRequest(http.MethodGet, "/", nil)); err != nil || !called {
			t.Fatal("reference rendering replaced application override")
		}
		called = false
		if err := configureRendering(app); err == nil {
			t.Fatal("duplicate setup accepted")
		}
		if _, _, err := app.ContentManager.LoadNotifications(httptest.NewRequest(http.MethodGet, "/", nil)); err != nil || !called {
			t.Fatal("duplicate setup replaced application override")
		}
	}
}
