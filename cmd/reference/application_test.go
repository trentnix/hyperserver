package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/config"
	framework "github.com/trentnix/hyperserver/pkg/application"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestReferenceRegistrationLoginAndContact(t *testing.T) {
	for _, htmx := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "HTMX"}[htmx], func(t *testing.T) {
			h := newHTTPHarness(t, func(cfg *config.Config) { cfg.Auth.RegisterRequiresVerification = false })
			status, redirect := http.StatusSeeOther, "Location"
			if htmx {
				status, redirect = http.StatusOK, "HX-Redirect"
			}
			w := h.request(http.MethodPost, "/auth/register/email", registrationForm(), htmx)
			if w.Code != status || w.Header().Get(redirect) != "/login" {
				t.Fatalf("registration: status=%d headers=%v body=%s", w.Code, w.Header(), w.Body)
			}
			if _, err := h.app.Accounts.GetByEmail(t.Context(), "person@example.invalid"); err != nil {
				t.Fatal(err)
			}
			w = h.request(http.MethodPost, "/auth/login/email", url.Values{
				"email": {"person@example.invalid"}, "password": {"TestPassword1!"},
			}, htmx)
			if w.Code != status || w.Header().Get(redirect) != "/" {
				t.Fatalf("login: status=%d headers=%v body=%s", w.Code, w.Header(), w.Body)
			}
			w = h.request(http.MethodGet, "/", nil, htmx)
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "person@example.invalid") {
				t.Fatal("home page lost the authenticated identity", w.Code, w.Body)
			}
			w = h.request(http.MethodPost, "/contact", url.Values{
				"name": {"Reader"}, "email": {"person@example.invalid"}, "message": {"Hello"},
			}, htmx)
			if w.Code != http.StatusOK {
				t.Fatal("contact submission failed", w.Code, w.Body)
			}
			h.assertRowCount(t, "hyperserver_contact_submission", 1)
			w = h.request(http.MethodPost, "/auth/logout", nil, htmx)
			if w.Code != status || w.Header().Get(redirect) != "/" {
				t.Fatal("logout failed", w.Code, w.Header())
			}
		})
	}
}

func TestReferenceConstructorRollsBackContactPool(t *testing.T) {
	for _, failure := range []string{"selection", "routes"} {
		t.Run(failure, func(t *testing.T) {
			cfg := config.Config{
				HTTP:     config.HTTPConfig{ListenHost: "127.0.0.1"},
				App:      config.AppConfig{WorkingDirectory: t.TempDir()},
				Database: config.DatabaseConfig{Driver: "sqlite3", Connection: "contacts.db"},
			}
			var captured *application
			cause := errors.New("module selection failed")
			app, err := newApplication(t.Context(), cfg, "", framework.Options{
				Modules: func(ctx context.Context, base *framework.Application) ([]handlers.Descriptor, error) {
					captured = &application{Application: base}
					catalog, err := siteModules(ctx, captured, "")
					if err != nil {
						return nil, err
					}
					if failure == "selection" {
						return nil, cause
					}
					return append(catalog, handlers.Descriptor{Name: "conflict", New: func() handlers.Handler {
						return &testModule{bind: func(routes *routing.Routes) {
							routes.HandleFunc("GET /contact", func(http.ResponseWriter, *http.Request) {})
						}}
					}}), nil
				},
			})
			if app != nil || err == nil || captured == nil || captured.contactDB == nil {
				t.Fatalf("app=%v err=%v captured=%v", app, err, captured)
			}
			if failure == "selection" && !errors.Is(err, cause) {
				t.Fatal("lost module-selection error", err)
			}
			if failure == "routes" && !strings.Contains(err.Error(), "conflicts") {
				t.Fatal("wrong route error", err)
			}
			if captured.contactDB.Ping() == nil {
				t.Fatal("failed constructor leaked contact storage")
			}
		})
	}
}

func TestReferenceConstructorRejectsInvalidAddressBeforeServices(t *testing.T) {
	for _, host := range []string{"", " \t", "localhost", "LOCALHOST", "0.0.0.0"} {
		cfg := config.Config{HTTP: config.HTTPConfig{ListenHost: host}, Auth: config.AuthConfig{Enabled: true}}
		app, err := newApplication(t.Context(), cfg, "", framework.Options{
			AccountProviders: map[string]user.StorageProvider{"sqlite": {Open: func(context.Context, map[string]string) (user.AccountRepository, func() error, error) {
				t.Fatal("opened accounts before validating the listen address")
				return nil, nil, nil
			}}},
		})
		want := "http.listenHost must be configured"
		if strings.TrimSpace(host) != "" {
			want = "loopback"
		}
		if app != nil || err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("host=%q app=%v err=%v", host, app, err)
		}
	}
}
