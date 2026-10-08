package server

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func accountProviderApplication(name string) *ApplicationServer {
	return &ApplicationServer{Config: &config.Config{Auth: config.AuthConfig{
		Enabled: true, AccountStorage: config.AccountStorageConfig{Provider: name},
	}}}
}

func TestAccountProviderSelection(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "startup")
	s := accountProviderApplication("custom")
	s.Config.Auth.AccountStorage.Options = map[string]string{"connection": "configured"}
	repository := &suppliedAccountRepository{}
	calls, closes := 0, 0
	err := s.RegisterAccountProvider("custom", func(got context.Context, options map[string]string) (user.AccountRepository, func() error, error) {
		calls++
		if got.Value(contextKey{}) != "startup" || options["connection"] != "configured" {
			t.Fatal("factory lost context or options")
		}
		if _, ok := got.Deadline(); !ok {
			t.Fatal("factory received no startup deadline")
		}
		options["connection"] = "changed by provider"
		return repository, func() error { closes++; return nil }, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterAccountProvider("unused", func(context.Context, map[string]string) (user.AccountRepository, func() error, error) {
		t.Fatal("unselected factory was called")
		return nil, nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.InitializeAccounts(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if s.AccountRepository != repository || calls != 1 || closes != 0 || s.Config.Auth.AccountStorage.Options["connection"] != "configured" {
		t.Fatalf("selection changed repository, configuration, or lifecycle: calls=%d, closes=%d", calls, closes)
	}
	for range 2 {
		if err := s.Shutdown(); err != nil {
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
		if err := s.RegisterAccountProvider(name, factory); err == nil {
			t.Fatalf("invalid or reserved name %q accepted", name)
		}
	}
	s := accountProviderApplication("custom")
	if err := s.RegisterAccountProvider("custom", nil); err == nil {
		t.Fatal("nil factory accepted")
	}
	if err := s.RegisterAccountProvider("custom", factory); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterAccountProvider("custom", factory); err == nil {
		t.Fatal("duplicate factory accepted")
	}
	if err := s.InitializeAccounts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterAccountProvider("late", factory); err == nil {
		t.Fatal("late registration accepted")
	}
}

func TestAccountProviderSelectionErrors(t *testing.T) {
	for _, name := range []string{"missing", "SQLite", " sqlite "} {
		s := accountProviderApplication(name)
		if err := s.InitializeAccounts(context.Background()); err == nil || !strings.Contains(err.Error(), "auth.accountStorage.provider") || s.AccountRepository != nil {
			t.Fatalf("unknown provider %q: error=%v, repository=%v", name, err, s.AccountRepository)
		}
	}
	s := accountProviderApplication("sqlite")
	s.Config.Auth.AccountStorage.Options = map[string]string{"connection": "must-not-appear-in-errors"}
	err := s.InitializeAccounts(context.Background())
	if err == nil || !strings.Contains(err.Error(), "auth.accountStorage.options") || strings.Contains(err.Error(), "must-not-appear-in-errors") {
		t.Fatalf("unsupported SQLite options error = %v", err)
	}
}

func TestAccountProviderExplicitSQLite(t *testing.T) {
	s := accountProviderApplication("sqlite")
	s.Database = accountSetupTestDB(t)
	if err := s.InitializeAccounts(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertAccountRepositoryReady(t, s.AccountRepository)
	if s.closeAccounts != nil {
		t.Fatal("SQLite claimed ownership of the shared pool")
	}
}

func TestAccountProviderSelectionBypassed(t *testing.T) {
	for _, scenario := range []string{"disabled", "supplied"} {
		t.Run(scenario, func(t *testing.T) {
			s := accountProviderApplication("unknown")
			if scenario == "disabled" {
				s.Config.Auth.Enabled = false
			} else {
				s.AccountRepository = &suppliedAccountRepository{}
			}
			repository := s.AccountRepository
			if err := s.InitializeAccounts(context.Background()); err != nil || s.AccountRepository != repository {
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
			if err := s.RegisterAccountProvider("custom", func(context.Context, map[string]string) (user.AccountRepository, func() error, error) {
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
			}); err != nil {
				t.Fatal(err)
			}

			err := s.InitializeAccounts(ctx)
			if err == nil || !errors.Is(err, cleanupErr) || closes != 1 || s.AccountRepository != nil {
				t.Fatalf("failed setup: error=%v, closes=%d, repository=%v", err, closes, s.AccountRepository)
			}
			if scenario == "error" && !errors.Is(err, setupErr) {
				t.Fatal("lost setup error:", err)
			}
			if scenario == "canceled during setup" && !errors.Is(err, context.Canceled) {
				t.Fatal("lost cancellation:", err)
			}
			if err := s.Shutdown(); err != nil || closes != 1 {
				t.Fatalf("shutdown repeated failed setup cleanup: %v, closes=%d", err, closes)
			}
			if err := s.InitializeAccounts(context.Background()); err != nil || s.AccountRepository != repository {
				t.Fatal("retry failed:", err)
			}
			if err := s.Shutdown(); !errors.Is(err, cleanupErr) || closes != 2 || calls != 2 {
				t.Fatalf("retry lifecycle: error=%v, closes=%d, calls=%d", err, closes, calls)
			}
		})
	}
}

func TestAccountProviderCanceledBeforeSetup(t *testing.T) {
	s := accountProviderApplication("custom")
	if err := s.RegisterAccountProvider("custom", func(context.Context, map[string]string) (user.AccountRepository, func() error, error) {
		t.Fatal("canceled setup called factory")
		return nil, nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.InitializeAccounts(ctx); !errors.Is(err, context.Canceled) || s.AccountRepository != nil {
		t.Fatalf("canceled setup: %v", err)
	}
}

func TestAccountProviderDeadline(t *testing.T) {
	for _, timeout := range []time.Duration{accountSetupTimeout, time.Second} {
		t.Run(timeout.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := accountProviderApplication("custom")
				closed := false
				if err := s.RegisterAccountProvider("custom", func(ctx context.Context, _ map[string]string) (user.AccountRepository, func() error, error) {
					<-ctx.Done()
					// A late success must be rejected and cleaned up too.
					return &suppliedAccountRepository{}, func() error { closed = true; return nil }, nil
				}); err != nil {
					t.Fatal(err)
				}
				ctx := context.Background()
				if timeout < accountSetupTimeout {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, timeout)
					defer cancel()
				}
				start := time.Now()
				if err := s.InitializeAccounts(ctx); !errors.Is(err, context.DeadlineExceeded) || s.AccountRepository != nil || !closed {
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
	closed := make(map[*ApplicationServer]bool)
	for _, s := range []*ApplicationServer{first, second} {
		s.Database = accountSetupTestDB(t)
		if err := s.RegisterAccountProvider("custom", func(context.Context, map[string]string) (user.AccountRepository, func() error, error) {
			return &suppliedAccountRepository{}, func() error {
				if err := s.Database.Ping(); err != nil {
					t.Fatal("shared pool closed before account provider:", err)
				}
				closed[s] = true
				return closeErr
			}, nil
		}); err != nil {
			t.Fatal(err)
		}
		if err := s.InitializeAccounts(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if err := first.Shutdown(); !errors.Is(err, closeErr) || !closed[first] || closed[second] {
		t.Fatalf("first shutdown: error=%v, closed=%v", err, closed)
	}
	if err := first.Database.Ping(); err == nil {
		t.Fatal("provider cleanup failure prevented shared pool closure")
	}
	if err := second.Database.Ping(); err != nil {
		t.Fatal("first shutdown affected second pool:", err)
	}
	if err := second.Shutdown(); !errors.Is(err, closeErr) || !closed[second] {
		t.Fatal("second shutdown failed:", err)
	}
}
