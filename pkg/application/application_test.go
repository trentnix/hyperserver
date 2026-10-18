package application_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/application"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

type accountStub struct{ user.AccountRepository }

type suppliedAccounts struct {
	user.AccountRepository
	closes int
}

func (a *suppliedAccounts) Close() error { a.closes++; return nil }

type storeStub struct {
	session.SessionStore
	close func() error
}

func (*storeStub) IsEnabled() bool { return true }
func (s *storeStub) Close() error  { return s.close() }

type senderStub struct{ closes int }

func (*senderStub) Send(context.Context, messaging.MailMessage) error { return nil }
func (s *senderStub) Close() error                                    { s.closes++; return nil }

func storageConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := config.Config{App: config.AppConfig{WorkingDirectory: t.TempDir()}}
	cfg.Auth.Enabled = true
	cfg.Auth.JwtKey = strings.Repeat("a", 64)
	cfg.Auth.VerificationTokenExpiration = time.Hour
	cfg.Auth.ResetTokenExpiration = time.Hour
	cfg.Auth.AccountStorage.Provider = "custom"
	cfg.HTTP.Session.Types = map[string]string{"default": "custom"}
	cfg.HTTP.Session.Stores = map[string]map[string]string{"custom": {"enabled": "true"}}
	return cfg
}

func TestNewWithoutOptionalServices(t *testing.T) {
	cfg := config.Config{App: config.AppConfig{WorkingDirectory: t.TempDir()}}
	app, err := application.New(t.Context(), cfg, application.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.Shutdown() })
	if app.Accounts != nil || app.Sessions != nil || app.Mail != nil || app.MailHealth != nil {
		t.Fatal("unconfigured services were created")
	}
	if app.Web == nil || app.ContentManager == nil {
		t.Fatal("core services missing")
	}
	entries, err := os.ReadDir(cfg.App.WorkingDirectory)
	if err != nil || len(entries) != 0 {
		t.Fatalf("unconfigured storage wrote files: %v %v", entries, err)
	}
}

func TestNewOpensBuiltInsAndClosesStorage(t *testing.T) {
	cfg := storageConfig(t)
	cfg.Auth.AccountStorage.Provider = ""
	cfg.Database = config.DatabaseConfig{Driver: "sqlite3", Connection: "accounts.db"}
	cfg.HTTP.Session.JwtKey = strings.Repeat("s", 64)
	cfg.HTTP.Session.TokenAge, cfg.HTTP.Session.CookieAge = time.Hour, time.Hour
	cfg.HTTP.Session.Types["default"] = "sqlitestore"
	cfg.HTTP.Session.Stores = map[string]map[string]string{
		"sqlitestore": {"enabled": "true", "connection": "sessions.db", "sessiontable": "sessions"},
	}
	// A syntactically valid but unreachable host must not be contacted at startup.
	cfg.Mail = config.MailConfig{Hostname: "smtp.invalid", Port: 587, User: "test", Password: "test", FromAddress: "sender@example.com"}
	app, err := application.New(t.Context(), cfg, application.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.Shutdown() })
	if app.Mail == nil || app.MailHealth == nil {
		t.Fatal("mail setup missing")
	}
	if err := app.Mail.ValidateConfig(); err != nil {
		t.Fatal(err)
	}
	u := &user.User{Email: "reader@example.com", Password: "stored-password-hash", RegistrationAuthType: "email"}
	if err := app.Accounts.Create(t.Context(), u); err != nil {
		t.Fatal(err)
	}
	if err := app.Sessions.CheckHealth(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"accounts.db", "sessions.db"} {
		if _, err := os.Stat(filepath.Join(cfg.App.WorkingDirectory, name)); err != nil {
			t.Fatal(err)
		}
	}

	if err := app.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Accounts.GetByID(t.Context(), u.ID); err == nil {
		t.Fatal("account storage remained open")
	}
	if err := app.Sessions.CheckHealth(t.Context()); err == nil {
		t.Fatal("session storage remained open")
	}
	if cfg.Auth.AccountStorage.Provider != "" || cfg.Database.Connection != "accounts.db" {
		t.Fatal("startup changed caller configuration")
	}
	app.Config.HTTP.Session.Types["default"] = "changed"
	if cfg.HTTP.Session.Types["default"] != "sqlitestore" {
		t.Fatal("application and caller share configuration")
	}
}

func TestSuppliedServicesBypassFactoriesAndKeepOwnership(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "shutdown", true: "startup failure"}[fail], func(t *testing.T) {
			cfg := storageConfig(t)
			closes := 0
			store := &storeStub{close: func() error { closes++; return nil }}
			manager, err := session.NewSessionManager(t.Context(), map[string]string{"default": "provided"}, map[string]session.StoreFactory{
				"provided": func(context.Context) (session.SessionStore, error) { return store, nil },
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { manager.Close() })
			sender := &senderStub{}
			mail, err := messaging.NewMailClientWithSender("sender@example.com", sender)
			if err != nil {
				t.Fatal(err)
			}
			probeErr := errors.New("custom probe")
			accounts := &suppliedAccounts{}
			services := application.Services{
				Accounts: accounts, Sessions: manager, Mail: mail,
				MailHealth: func(context.Context) error { return probeErr },
			}
			cfg.Mail.Timeout = -time.Second
			if fail {
				services.Mail = nil // Fail after receiving the supplied storage services.
			}
			app, err := application.New(t.Context(), cfg, application.Options{Services: services})
			// The configured "custom" providers do not exist. Supplied services
			// must bypass provider lookup, validation, and initialization.
			if fail {
				if app != nil || err == nil || !strings.Contains(err.Error(), "prepare mail") {
					t.Fatalf("expected mail setup failure: app=%v err=%v", app, err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if app.Accounts != services.Accounts || app.Sessions != manager || app.Mail != mail || !errors.Is(app.MailHealth(t.Context()), probeErr) {
					t.Fatal("supplied services were replaced")
				}
				if err := app.Shutdown(); err != nil {
					t.Fatal(err)
				}
			}
			if accounts.closes != 0 || closes != 0 || sender.closes != 0 {
				t.Fatal("closed caller-owned services")
			}
		})
	}
}

func TestStartupFailureClosesOwnedResources(t *testing.T) {
	for _, stage := range []string{"accounts", "sessions", "mail", "canceled accounts", "canceled sessions"} {
		t.Run(stage, func(t *testing.T) {
			cfg := storageConfig(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			cause, accountCloseErr, sessionCloseErr := errors.New("open failed"), errors.New("account close failed"), errors.New("session close failed")
			var closed []string
			options := application.Options{
				Modules: func(context.Context, *application.Application) ([]handlers.Descriptor, error) {
					t.Fatal("selected modules after service startup failed")
					return nil, nil
				},
				AccountProviders: map[string]user.StorageProvider{"custom": {Open: func(context.Context, map[string]string) (user.AccountRepository, func() error, error) {
					cleanup := func() error { closed = append(closed, "accounts"); return accountCloseErr }
					if stage == "accounts" {
						return nil, cleanup, cause
					}
					if stage == "canceled accounts" {
						cancel()
					}
					return &accountStub{}, cleanup, nil
				}}},
				SessionProviders: map[string]session.ConfiguredProvider{"custom": {Open: func(context.Context, map[string]string) (session.SessionStore, error) {
					if stage == "sessions" {
						return nil, cause
					}
					if stage == "canceled sessions" {
						cancel()
					}
					return &storeStub{close: func() error { closed = append(closed, "sessions"); return sessionCloseErr }}, nil
				}}},
			}
			if stage == "mail" {
				cfg.Mail.Timeout = -time.Second
			}
			app, err := application.New(ctx, cfg, options)
			if app != nil || err == nil || !errors.Is(err, accountCloseErr) {
				t.Fatalf("lost failure or cleanup: app=%v err=%v", app, err)
			}
			want := []string{"accounts"}
			if stage == "mail" || stage == "canceled sessions" {
				want = []string{"sessions", "accounts"}
				if !errors.Is(err, sessionCloseErr) {
					t.Fatal("lost session cleanup error", err)
				}
			}
			if !reflect.DeepEqual(closed, want) {
				t.Fatalf("cleanup order = %v, want %v", closed, want)
			}
			if strings.HasPrefix(stage, "canceled") && !errors.Is(err, context.Canceled) {
				t.Fatal("lost cancellation", err)
			}
			if (stage == "accounts" || stage == "sessions") && !errors.Is(err, cause) {
				t.Fatal("lost provider error", err)
			}
		})
	}
}

func TestNewRejectsInvalidSetupBeforeOpeningServices(t *testing.T) {
	for _, mode := range []string{"config", "paths", "canceled", "typed nil"} {
		t.Run(mode, func(t *testing.T) {
			cfg := storageConfig(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			options := application.Options{AccountProviders: map[string]user.StorageProvider{"custom": {Open: func(context.Context, map[string]string) (user.AccountRepository, func() error, error) {
				t.Fatal("invalid setup opened storage")
				return nil, nil, nil
			}}}}
			switch mode {
			case "config":
				timeout := time.Duration(0)
				cfg.App.InitializationTimeout = &timeout
			case "paths":
				cfg.App.WorkingDirectory = filepath.Join(cfg.App.WorkingDirectory, "missing")
			case "canceled":
				cancel()
			case "typed nil":
				options.Services.Accounts = (*accountStub)(nil)
			}
			app, err := application.New(ctx, cfg, options)
			if app != nil || err == nil {
				t.Fatalf("invalid setup accepted: app=%v err=%v", app, err)
			}
		})
	}
}

func TestStartupDeadline(t *testing.T) {
	for _, mode := range []string{"default", "configured", "parent", "expiration"} {
		t.Run(mode, func(t *testing.T) {
			cfg := storageConfig(t)
			cfg.HTTP.Session.Types, cfg.HTTP.Session.Stores = nil, nil
			ctx := t.Context()
			budget := 10 * time.Second
			if mode != "default" {
				budget = time.Minute
				cfg.App.InitializationTimeout = &budget
			}
			var parentDeadline time.Time
			if mode == "parent" {
				parentDeadline = time.Now().Add(5 * time.Second)
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, parentDeadline)
				defer cancel()
			}
			if mode == "expiration" {
				budget = 20 * time.Millisecond
			}
			closed := false
			started := time.Now()
			app, err := application.New(ctx, cfg, application.Options{AccountProviders: map[string]user.StorageProvider{"custom": {Open: func(got context.Context, _ map[string]string) (user.AccountRepository, func() error, error) {
				deadline, ok := got.Deadline()
				if !ok {
					t.Fatal("no startup deadline")
				}
				if mode == "parent" {
					if !deadline.Equal(parentDeadline) {
						t.Fatal("parent deadline was replaced")
					}
				} else if deadline.Before(started.Add(budget)) || deadline.After(time.Now().Add(budget)) {
					t.Fatalf("unexpected deadline %v for budget %v", deadline, budget)
				}
				if mode == "expiration" {
					<-got.Done()
				}
				return &accountStub{}, func() error { closed = true; return nil }, nil
			}}}})
			if mode == "expiration" {
				if app != nil || !errors.Is(err, context.DeadlineExceeded) || !closed {
					t.Fatalf("expired startup: app=%v err=%v closed=%v", app, err, closed)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if closed {
					t.Fatal("successful startup closed storage")
				}
				if err := app.Shutdown(); err != nil || !closed {
					t.Fatalf("shutdown: err=%v closed=%v", err, closed)
				}
			}
		})
	}
}

func TestRunClosesServicesAfterConsumersOnce(t *testing.T) {
	cfg := storageConfig(t)
	var closed []string
	app, err := application.New(t.Context(), cfg, application.Options{
		AccountProviders: map[string]user.StorageProvider{"custom": {Open: func(context.Context, map[string]string) (user.AccountRepository, func() error, error) {
			return &accountStub{}, func() error { closed = append(closed, "accounts"); return nil }, nil
		}}},
		SessionProviders: map[string]session.ConfiguredProvider{"custom": {Open: func(context.Context, map[string]string) (session.SessionStore, error) {
			return &storeStub{close: func() error { closed = append(closed, "sessions"); return nil }}, nil
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	app.OnShutdown("consumer", func(context.Context) error { closed = append(closed, "consumer"); return nil })
	cause := errors.New("module setup failed")
	err = app.Run(t.Context(), func(context.Context) (*http.Server, net.Listener, error) { return nil, nil, cause })
	if !errors.Is(err, cause) {
		t.Fatal("lost preparation failure", err)
	}
	if err := app.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if want := []string{"consumer", "sessions", "accounts"}; !reflect.DeepEqual(closed, want) {
		t.Fatalf("cleanup = %v, want %v", closed, want)
	}
}
