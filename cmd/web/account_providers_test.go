package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/internal/reference"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func accountProviderApplication(name string) *accountSetupHarness {
	return newAccountSetup(testApplicationWithPool(&server.ApplicationServer{Config: &config.Config{Auth: config.AuthConfig{
		Enabled: true, AccountStorage: config.AccountStorageConfig{Provider: name},
	}}}, nil))
}

func TestAccountProviderSelection(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "startup")
	s := accountProviderApplication("custom")
	s.app.Config.Auth.AccountStorage.Options = map[string]string{"connection": "configured"}
	repository := &suppliedAccountRepository{}
	calls, closes := 0, 0
	err := s.RegisterProvider("custom", reference.AccountProvider{Open: func(got context.Context, options map[string]string) (user.AccountRepository, func() error, error) {
		calls++
		if got.Value(contextKey{}) != "startup" || options["connection"] != "configured" {
			t.Fatal("factory lost context or options")
		}
		if _, ok := got.Deadline(); !ok {
			t.Fatal("factory received no startup deadline")
		}
		options["connection"] = "changed by provider"
		return repository, func() error { closes++; return nil }, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterProvider("unused", reference.AccountProvider{Open: func(context.Context, map[string]string) (user.AccountRepository, func() error, error) {
		t.Fatal("unselected factory was called")
		return nil, nil, nil
	}}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.Initialize(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if s.app.AccountRepository != repository || calls != 1 || closes != 0 || s.app.Config.Auth.AccountStorage.Options["connection"] != "configured" {
		t.Fatalf("selection changed repository, configuration, or lifecycle: calls=%d, closes=%d", calls, closes)
	}
	for range 2 {
		if err := s.app.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}
	if closes != 1 {
		t.Fatalf("cleanup calls = %d, want 1", closes)
	}
}

func TestAccountProviderRegistration(t *testing.T) {
	factory := func(context.Context, map[string]string) (user.AccountRepository, func() error, error) {
		return &suppliedAccountRepository{}, nil, nil
	}
	for _, name := range []string{"", " custom", "custom ", "sqlite"} {
		s := accountProviderApplication("custom")
		if err := s.RegisterProvider(name, reference.AccountProvider{Open: factory}); err == nil {
			t.Fatalf("invalid or reserved name %q accepted", name)
		}
	}
	s := accountProviderApplication("custom")
	if err := s.RegisterProvider("custom", reference.AccountProvider{}); err == nil {
		t.Fatal("nil factory accepted")
	}
	if err := s.RegisterProvider("custom", reference.AccountProvider{Open: factory}); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterProvider("custom", reference.AccountProvider{Open: factory}); err == nil {
		t.Fatal("duplicate factory accepted")
	}
}

func TestAccountProviderSelectionErrors(t *testing.T) {
	for _, name := range []string{"missing", "SQLite", " sqlite "} {
		s := accountProviderApplication(name)
		if err := s.Initialize(context.Background()); err == nil || !strings.Contains(err.Error(), "auth.accountStorage.provider") || s.app.AccountRepository != nil {
			t.Fatalf("unknown provider %q: error=%v, repository=%v", name, err, s.app.AccountRepository)
		}
	}
	s := accountProviderApplication("sqlite")
	s.app.Config.Auth.AccountStorage.Options = map[string]string{"connection": "must-not-appear-in-errors"}
	err := s.Initialize(context.Background())
	if err == nil || !strings.Contains(err.Error(), "auth.accountStorage.options") || strings.Contains(err.Error(), "must-not-appear-in-errors") {
		t.Fatalf("unsupported SQLite options error = %v", err)
	}
}

func TestAccountProviderExplicitSQLite(t *testing.T) {
	s := accountProviderApplication("sqlite")
	s.app.Database = accountSetupTestDB(t)
	if err := s.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertAccountRepositoryReady(t, s.app.AccountRepository)
	// The test owns this pool. Account setup must not acquire ownership.
	pool := s.app.Database
	if err := s.app.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if err := pool.Ping(); err != nil {
		t.Fatal("SQLite account setup closed the borrowed pool:", err)
	}
}

func TestAccountProviderSelectionBypassed(t *testing.T) {
	for _, scenario := range []string{"disabled", "supplied"} {
		t.Run(scenario, func(t *testing.T) {
			s := accountProviderApplication("unknown")
			if scenario == "disabled" {
				s.app.Config.Auth.Enabled = false
			} else {
				s.app.AccountRepository = &suppliedAccountRepository{}
			}
			repository := s.app.AccountRepository
			if err := s.Initialize(context.Background()); err != nil || s.app.AccountRepository != repository {
				t.Fatalf("bypassed selection changed repository or failed: %v", err)
			}
		})
	}
}

func TestAccountProviderFailureCleanupAndRetry(t *testing.T) {
	setupErr, cleanupErr := errors.New("setup failed"), errors.New("cleanup failed")
	for _, scenario := range []string{"error", "nil repository", "canceled during setup"} {
		t.Run(scenario, func(t *testing.T) {
			s := accountProviderApplication("custom")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls, closes := 0, 0
			repository := &suppliedAccountRepository{}
			if err := s.RegisterProvider("custom", reference.AccountProvider{Open: func(context.Context, map[string]string) (user.AccountRepository, func() error, error) {
				calls++
				cleanup := func() error { closes++; return cleanupErr }
				if calls > 1 {
					return repository, cleanup, nil
				}
				switch scenario {
				case "error":
					return repository, cleanup, setupErr
				case "nil repository":
					return nil, cleanup, nil
				default:
					cancel()
					return repository, cleanup, nil
				}
			}}); err != nil {
				t.Fatal(err)
			}

			err := s.Initialize(ctx)
			if err == nil || !errors.Is(err, cleanupErr) || closes != 1 || s.app.AccountRepository != nil {
				t.Fatalf("failed setup: error=%v, closes=%d, repository=%v", err, closes, s.app.AccountRepository)
			}
			if scenario == "error" && !errors.Is(err, setupErr) {
				t.Fatal("lost setup error:", err)
			}
			if scenario == "canceled during setup" && !errors.Is(err, context.Canceled) {
				t.Fatal("lost cancellation:", err)
			}
			if err := s.app.Shutdown(); err != nil || closes != 1 {
				t.Fatalf("shutdown repeated failed setup cleanup: %v, closes=%d", err, closes)
			}
			if err := s.Initialize(context.Background()); err != nil || s.app.AccountRepository != repository {
				t.Fatal("retry failed:", err)
			}
			if err := s.app.Shutdown(); !errors.Is(err, cleanupErr) || closes != 2 || calls != 2 {
				t.Fatalf("retry lifecycle: error=%v, closes=%d, calls=%d", err, closes, calls)
			}
		})
	}
}

func TestAccountProviderCanceledBeforeSetup(t *testing.T) {
	s := accountProviderApplication("custom")
	if err := s.RegisterProvider("custom", reference.AccountProvider{Open: func(context.Context, map[string]string) (user.AccountRepository, func() error, error) {
		t.Fatal("canceled setup called factory")
		return nil, nil, nil
	}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Initialize(ctx); !errors.Is(err, context.Canceled) || s.app.AccountRepository != nil {
		t.Fatalf("canceled setup: %v", err)
	}
}

func TestAccountProviderDeadline(t *testing.T) {
	for _, timeout := range []time.Duration{10 * time.Second, time.Second} {
		t.Run(timeout.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := accountProviderApplication("custom")
				closed := false
				if err := s.RegisterProvider("custom", reference.AccountProvider{Open: func(ctx context.Context, _ map[string]string) (user.AccountRepository, func() error, error) {
					<-ctx.Done()
					// A late success must be rejected and cleaned up too.
					return &suppliedAccountRepository{}, func() error { closed = true; return nil }, nil
				}}); err != nil {
					t.Fatal(err)
				}
				ctx := context.Background()
				if timeout < 10*time.Second {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, timeout)
					defer cancel()
				}
				start := time.Now()
				if err := s.Initialize(ctx); !errors.Is(err, context.DeadlineExceeded) || s.app.AccountRepository != nil || !closed {
					t.Fatalf("deadline setup: error=%v, closed=%t", err, closed)
				}
				if elapsed := time.Since(start); elapsed != timeout {
					t.Fatalf("setup waited %s, want %s", elapsed, timeout)
				}
			})
		})
	}
}

func TestAccountProviderOwnershipAndIsolation(t *testing.T) {
	first, second := accountProviderApplication("custom"), accountProviderApplication("custom")
	closeErr := errors.New("provider close failed")
	closed := make(map[*accountSetupHarness]bool)
	for _, s := range []*accountSetupHarness{first, second} {
		s.app.ownDatabase(accountSetupTestDB(t))
		if err := s.RegisterProvider("custom", reference.AccountProvider{Open: func(context.Context, map[string]string) (user.AccountRepository, func() error, error) {
			return &suppliedAccountRepository{}, func() error {
				if err := s.app.Database.Ping(); err != nil {
					t.Fatal("shared pool closed before account provider:", err)
				}
				closed[s] = true
				return closeErr
			}, nil
		}}); err != nil {
			t.Fatal(err)
		}
		if err := s.Initialize(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if err := first.app.Shutdown(); !errors.Is(err, closeErr) || !closed[first] || closed[second] {
		t.Fatalf("first shutdown: error=%v, closed=%v", err, closed)
	}
	if err := first.app.Database.Ping(); err == nil {
		t.Fatal("provider cleanup failure prevented shared pool closure")
	}
	if err := second.app.Database.Ping(); err != nil {
		t.Fatal("first shutdown affected second pool:", err)
	}
	if err := second.app.Shutdown(); !errors.Is(err, closeErr) || !closed[second] {
		t.Fatal("second shutdown failed:", err)
	}
}

func TestAccountSetupRegistersCleanupBeforeConsumers(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		canceled bool
	}{
		{"shutdown", false},
		{"canceled shutdown", true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			s := accountProviderApplication("custom")
			var order []string
			closeErr := errors.New("account cleanup failed")
			s.app.OnShutdown("shared resource", func(context.Context) error {
				order = append(order, "shared resource")
				return nil
			})
			if err := s.RegisterProvider("custom", reference.AccountProvider{Open: func(context.Context, map[string]string) (user.AccountRepository, func() error, error) {
				return &suppliedAccountRepository{}, func() error {
					order = append(order, "accounts")
					return closeErr
				}, nil
			}}); err != nil {
				t.Fatal(err)
			}
			if err := s.Initialize(t.Context()); err != nil {
				t.Fatal(err)
			}
			// A fresh setup helper must preserve the published repository and its owner.
			if err := newAccountSetup(s.app).Initialize(t.Context()); err != nil {
				t.Fatal(err)
			}
			s.app.OnShutdown("consumer", func(context.Context) error {
				order = append(order, "consumer")
				return nil
			})

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if scenario.canceled {
				cancel()
			}
			err := s.app.ShutdownContext(ctx)
			if !errors.Is(err, closeErr) || errors.Is(err, context.Canceled) != scenario.canceled || strings.Join(order, ",") != "consumer,accounts,shared resource" {
				t.Fatalf("cleanup order=%v, error=%v", order, err)
			}
			if err := s.app.Shutdown(); err != nil || len(order) != 3 {
				t.Fatalf("repeated shutdown: order=%v, error=%v", order, err)
			}
		})
	}
}
