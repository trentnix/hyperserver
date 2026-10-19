package application_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/trentnix/hyperserver/auth"
	email "github.com/trentnix/hyperserver/auth/modules/email"
	"github.com/trentnix/hyperserver/auth/password"
	"github.com/trentnix/hyperserver/pkg/application"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestAuthenticationSetupWithAccountAdmission(t *testing.T) {
	for _, htmx := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "HTMX"}[htmx], func(t *testing.T) {
			cfg := authenticationConfig(t)
			mail, err := messaging.NewMailClientWithSender("sender@example.com", &verificationSender{})
			if err != nil {
				t.Fatal(err)
			}
			var allowedKey string
			selected, admissions := 0, 0
			app, err := application.New(t.Context(), cfg, application.Options{
				Services: application.Services{Mail: mail},
				Modules: func(ctx context.Context, app *application.Application) ([]handlers.Descriptor, error) {
					catalog, err := authenticationModules(ctx, app)
					// Auth deliberately initializes first. The policy runs only on requests.
					catalog = append(catalog, handlers.Descriptor{Name: "access", New: func() handlers.Handler {
						return &moduleStub{init: func(context.Context, *server.ApplicationServer) error {
							allowedKey = user.EmailKey("Admin@EXAMPLE.com")
							return nil
						}}
					}})
					return catalog, err
				},
				AuthProviders: func(ctx context.Context, app *application.Application) ([]auth.Descriptor, error) {
					selected++
					if _, ok := ctx.Deadline(); !ok || ctx.Err() != nil || allowedKey != "" {
						t.Fatal("provider selection ran outside the startup phase")
					}
					if app.Accounts == nil || app.Sessions == nil || app.Mail != mail || app.Sessions.CheckHealth(ctx) != nil {
						t.Fatal("provider selection did not receive ready services")
					}
					return []auth.Descriptor{email.Provider(app.Accounts, app.Sessions, app.Mail, email.Options{
						AllowAccount: func(ctx context.Context, address string) (bool, error) {
							admissions++
							if allowedKey == "" || ctx.Err() != nil {
								t.Fatal("admission ran before initialization or retained a startup context")
							}
							return user.EmailKey(address) == allowedKey, nil
						},
					})}, nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { app.Shutdown() })
			if selected != 1 || admissions != 0 {
				t.Fatal("incorrect provider selection or admission timing")
			}
			request := browserRequests(t, app, htmx)
			fields := url.Values{"email": {"other@example.com"}, "password": {"TestPassword1!"}, "passwordMatch": {"TestPassword1!"}}
			assertDenied := func(path string) {
				t.Helper()
				w := request(http.MethodPost, path, fields)
				if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Account access is not available") ||
					w.Header().Get("Location") != "" || w.Header().Get("HX-Redirect") != "" || w.Header().Get("Set-Cookie") != "" {
					t.Fatal("admission failure did not render the form without authentication", w.Code, w.Header(), w.Body)
				}
			}
			assertDenied("/auth/register/email")
			if _, err := app.Accounts.GetByEmail(t.Context(), fields.Get("email")); !errors.Is(err, user.ErrUserNotFound) {
				t.Fatal("rejected registration created an account", err)
			}
			hash, err := password.HashPassword(fields.Get("password"))
			if err != nil {
				t.Fatal(err)
			}
			if err := app.Accounts.Create(t.Context(), &user.User{Email: fields.Get("email"), Password: hash, Verified: true}); err != nil {
				t.Fatal(err)
			}
			assertDenied("/auth/login/email")
			fields.Set("email", "ADMIN@example.com")
			for _, path := range []string{"/auth/register/email", "/auth/login/email"} {
				if path == "/auth/login/email" {
					bad := url.Values{"email": {fields.Get("email")}, "password": {"WrongPassword1!"}}
					w := request(http.MethodPost, path, bad)
					if !strings.Contains(w.Body.String(), "Invalid login/password") || w.Header().Get("Location") != "" || w.Header().Get("HX-Redirect") != "" {
						t.Fatal("admission bypassed password verification", w.Header(), w.Body)
					}
				}
				w := request(http.MethodPost, path, fields)
				if w.Header().Get("Location") == "" && w.Header().Get("HX-Redirect") == "" {
					t.Fatal("allowed account could not register or log in", w.Code, w.Body)
				}
			}
			if admissions != 5 {
				t.Fatalf("admission calls=%d, want 5", admissions)
			}
		})
	}
}

func TestAuthProviderSelectionFailureCleanup(t *testing.T) {
	for _, mode := range []string{"error", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			cfg := authenticationConfig(t)
			cause := errors.New("provider selection failed")
			var captured *application.Application
			closes := 0
			app, err := application.New(ctx, cfg, application.Options{
				Modules: authenticationModules,
				AuthProviders: func(ctx context.Context, app *application.Application) ([]auth.Descriptor, error) {
					captured = app
					app.OnShutdown("provider resource", func(ctx context.Context) error {
						if ctx.Err() != nil {
							t.Error("cleanup received canceled startup context")
						}
						closes++
						return nil
					})
					if mode == "cancel" {
						cancel()
					}
					return nil, cause
				},
			})
			if app != nil || err == nil || captured == nil || closes != 1 {
				t.Fatalf("app=%v error=%v captured=%v closes=%d", app, err, captured, closes)
			}
			if !errors.Is(err, cause) || mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("lost selection or context error", err)
			}
			if err := captured.Sessions.CheckHealth(t.Context()); err == nil {
				t.Fatal("owned sessions remained open")
			}
			if _, err := captured.Accounts.GetByEmail(t.Context(), "admin@example.com"); err == nil || !strings.Contains(err.Error(), "database is closed") {
				t.Fatal("owned accounts remained open", err)
			}
		})
	}
}

func TestAuthProviderSelectionDeadlineCleanup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Keep deadline behavior independent of database startup and runner speed.
		cfg := storageConfig(t)
		timeout := time.Second
		cfg.App.InitializationTimeout = &timeout
		var closed []string
		var captured *application.Application
		start := time.Now()
		app, err := application.New(t.Context(), cfg, application.Options{
			AccountProviders: map[string]user.StorageProvider{"custom": {Open: func(context.Context, map[string]string) (user.AccountRepository, func() error, error) {
				return &accountStub{}, func() error { closed = append(closed, "accounts"); return nil }, nil
			}}},
			SessionProviders: map[string]session.ConfiguredProvider{"custom": {Open: func(context.Context, map[string]string) (session.SessionStore, error) {
				return &storeStub{close: func() error { closed = append(closed, "sessions"); return nil }}, nil
			}}},
			Modules: authenticationModules,
			AuthProviders: func(ctx context.Context, app *application.Application) ([]auth.Descriptor, error) {
				captured = app
				app.OnShutdown("provider resource", func(ctx context.Context) error {
					if ctx.Err() != nil {
						t.Error("cleanup received expired startup context")
					}
					closed = append(closed, "provider")
					return nil
				})
				<-ctx.Done()
				return nil, nil // Late success must still abort startup and close resources.
			},
		})
		if app != nil || !errors.Is(err, context.DeadlineExceeded) || captured == nil {
			t.Fatalf("app=%v error=%v captured=%v", app, err, captured)
		}
		if time.Since(start) != timeout {
			t.Fatalf("elapsed=%v, want %v", time.Since(start), timeout)
		}
		if err := captured.Shutdown(); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(closed, []string{"provider", "sessions", "accounts"}) {
			t.Fatal("resources must close once in reverse order", closed)
		}
	})
}

func TestAuthProviderSelectionRequiresActiveAuth(t *testing.T) {
	for _, mode := range []string{"disabled", "unselected", "canceled before selection"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			cfg := authenticationConfig(t)
			if mode == "disabled" {
				cfg.Auth.Enabled = false
			}
			app, err := application.New(ctx, cfg, application.Options{
				Modules: func(ctx context.Context, app *application.Application) ([]handlers.Descriptor, error) {
					if mode == "unselected" {
						return nil, nil
					}
					catalog, err := authenticationModules(ctx, app)
					if mode == "canceled before selection" {
						cancel()
					}
					return catalog, err
				},
				AuthProviders: func(context.Context, *application.Application) ([]auth.Descriptor, error) {
					t.Fatal("inactive authentication or canceled startup selected providers")
					return nil, nil
				},
			})
			if app != nil {
				t.Cleanup(func() { app.Shutdown() })
			}
			if mode == "canceled before selection" {
				if app != nil || !errors.Is(err, context.Canceled) {
					t.Fatal("canceled startup succeeded", app, err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}
