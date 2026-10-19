package application_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/application"
	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/messages"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/services/flash"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func notificationPage(t *testing.T, app *application.Application) {
	t.Helper()
	files := fstest.MapFS{"notifications.html": {Data: []byte(`{{define "notifications"}}Page{{range .Notifications}}<p>{{.Message}}</p>{{end}}{{end}}`)}}
	view, err := content.PrepareView(app.ContentManager, "notifications", content.TemplatePath{FS: files, Path: "notifications.html"})
	if err != nil {
		t.Fatal(err)
	}
	app.Web.HandleFunc("GET /notifications", func(w http.ResponseWriter, r *http.Request) {
		if err := view.Content(r, nil).Render(w, r); err != nil {
			t.Error(err)
			http.Error(w, "render failed", http.StatusInternalServerError)
		}
	})
}

func TestConfiguredNotifications(t *testing.T) {
	for _, authEnabled := range []bool{false, true} {
		for _, enabled := range []bool{false, true} {
			for _, htmx := range []bool{false, true} {
				t.Run(fmt.Sprintf("auth=%t/notifications=%t/htmx=%t", authEnabled, enabled, htmx), func(t *testing.T) {
					cfg := authenticationConfig(t)
					cfg.Auth.Enabled = authEnabled
					cfg.App.RenderNotifications = enabled
					app, err := application.New(t.Context(), cfg, application.Options{Modules: authenticationModules})
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { app.Shutdown() })
					if (app.ContentManager.LoadNotifications != nil) != enabled {
						t.Fatal("notification loader does not match configuration")
					}
					notificationPage(t, app)
					app.Web.HandleFunc("POST /notifications", func(w http.ResponseWriter, r *http.Request) {
						if err := flash.AddSuccessNotification(w, r, "Saved notification"); err != nil {
							t.Error(err)
							return
						}
						w.WriteHeader(http.StatusNoContent)
					})
					app.Web.HandleFunc("GET /pending", func(w http.ResponseWriter, r *http.Request) {
						pending, err := flash.GetNotifications(w, r)
						if err != nil {
							t.Error(err)
							return
						}
						fmt.Fprint(w, len(pending))
					})

					request := browserRequests(t, app, htmx)
					if w := request(http.MethodPost, "/notifications", nil); w.Code != http.StatusNoContent {
						t.Fatal("could not store notification", w.Code, w.Body)
					}
					for visit := range 2 {
						w := request(http.MethodGet, "/notifications", nil)
						if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "Saved notification") != (enabled && visit == 0) {
							t.Fatalf("visit %d: status=%d body=%s", visit, w.Code, w.Body)
						}
					}
					wantPending := "1"
					if enabled {
						wantPending = "0"
					}
					if w := request(http.MethodGet, "/pending", nil); w.Code != http.StatusOK || w.Body.String() != wantPending {
						t.Fatalf("pending notifications: status=%d body=%s", w.Code, w.Body)
					}
				})
			}
		}
	}
}

func TestCustomNotificationsOverrideConfiguration(t *testing.T) {
	for _, authEnabled := range []bool{false, true} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("auth=%t/notifications=%t", authEnabled, enabled), func(t *testing.T) {
				cfg := config.Config{}
				if authEnabled {
					cfg = authenticationConfig(t)
				}
				cfg.App.RenderNotifications = enabled
				loads, acknowledgments := 0, 0
				app, err := application.New(t.Context(), cfg, application.Options{Modules: func(ctx context.Context, app *application.Application) ([]handlers.Descriptor, error) {
					app.ContentManager.LoadNotifications = func(*http.Request) ([]messages.Notification, func(http.ResponseWriter) error, error) {
						loads++
						return []messages.Notification{messages.NewNotification("Custom notification", messages.NotificationTypeSuccess)}, func(http.ResponseWriter) error {
							acknowledgments++
							return nil
						}, nil
					}
					return authenticationModules(ctx, app)
				}})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { app.Shutdown() })
				if !authEnabled && app.Sessions != nil {
					t.Fatal("custom notifications unexpectedly opened sessions")
				}
				notificationPage(t, app)
				w := browserRequests(t, app, false)(http.MethodGet, "/notifications", nil)
				if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Custom notification") || loads != 1 || acknowledgments != 1 {
					t.Fatalf("status=%d loads=%d acknowledgments=%d body=%s", w.Code, loads, acknowledgments, w.Body)
				}
			})
		}
	}
}

func TestNotificationSetupFailureClosesOwnedResources(t *testing.T) {
	for _, mode := range []string{"missing manager", "missing store"} {
		t.Run(mode, func(t *testing.T) {
			cfg := storageConfig(t)
			cfg.App.RenderNotifications = true
			cfg.HTTP.Session.Types, cfg.HTTP.Session.Stores = nil, nil
			var closed []string
			closeErr := errors.New("account close failure")
			options := application.Options{
				AccountProviders: map[string]user.StorageProvider{"custom": {Open: func(context.Context, map[string]string) (user.AccountRepository, func() error, error) {
					return &accountStub{}, func() error { closed = append(closed, "accounts"); return closeErr }, nil
				}}},
				Modules: func(_ context.Context, app *application.Application) ([]handlers.Descriptor, error) {
					app.OnShutdown("callback", func(context.Context) error { closed = append(closed, "callback"); return nil })
					return []handlers.Descriptor{{Name: "uninitialized", New: func() handlers.Handler {
						t.Fatal("initialized modules with invalid notification dependencies")
						return nil
					}}}, nil
				},
			}
			storeCloses := 0
			if mode == "missing store" {
				manager, err := session.NewSessionManager(t.Context(), map[string]string{"other": "custom"}, map[string]session.StoreFactory{
					"custom": func(context.Context) (session.SessionStore, error) {
						return &storeStub{close: func() error { storeCloses++; return nil }}, nil
					},
				})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { manager.Close() })
				options.Services.Sessions = manager
			}

			app, err := application.New(t.Context(), cfg, options)
			if app != nil || err == nil || !strings.Contains(err.Error(), "prepare notifications: flash messages require session") || !errors.Is(err, closeErr) {
				t.Fatalf("app=%v err=%v", app, err)
			}
			if !reflect.DeepEqual(closed, []string{"callback", "accounts"}) || storeCloses != 0 {
				t.Fatalf("cleanup order=%v borrowed store closes=%d", closed, storeCloses)
			}
		})
	}
}
