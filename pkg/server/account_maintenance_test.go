package server

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

type tokenMaintenanceFunc func(context.Context, int) (int64, error)

func (f tokenMaintenanceFunc) DeleteExpiredTokens(ctx context.Context, limit int) (int64, error) {
	return f(ctx, limit)
}

func TestAccountMaintenanceLifecycle(t *testing.T) {
	openErr, deleteErr, closeErr := errors.New("open failed"), errors.New("delete failed"), errors.New("close failed")
	for _, scenario := range []string{"success", "open failure", "delete failure", "close failure", "nil maintenance", "canceled before open", "canceled after open", "unsupported", "disabled", "invalid limit"} {
		t.Run(scenario, func(t *testing.T) {
			s := accountProviderApplication("custom")
			s.Config.Auth.AccountStorage.Options = map[string]string{"connection": "custom-storage"}
			type contextKey struct{}
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), contextKey{}, "maintenance"))
			defer cancel()
			opens, deletes, closes := 0, 0, 0
			provider := AccountProvider{
				Open: func(context.Context, map[string]string) (user.AccountRepository, func() error, error) {
					t.Fatal("maintenance initialized runtime storage")
					return nil, nil, nil
				},
				OpenMaintenance: func(got context.Context, options map[string]string) (user.TokenMaintenance, func() error, error) {
					opens++
					if got != ctx || options["connection"] != "custom-storage" {
						t.Fatal("maintenance lost context or configuration")
					}
					options["connection"] = "provider-modified"
					closeStore := func() error {
						closes++
						if scenario == "close failure" || scenario == "open failure" || scenario == "delete failure" {
							return closeErr
						}
						return nil
					}
					if scenario == "open failure" {
						return nil, closeStore, openErr
					}
					if scenario == "nil maintenance" {
						return nil, closeStore, nil
					}
					if scenario == "canceled after open" {
						cancel()
					}
					return tokenMaintenanceFunc(func(got context.Context, limit int) (int64, error) {
						deletes++
						if got != ctx || limit != 5 {
							t.Fatal("maintenance lost context or batch limit")
						}
						if scenario == "delete failure" {
							return 2, deleteErr
						}
						return 3, nil
					}), closeStore, nil
				},
			}
			limit := 5
			switch scenario {
			case "canceled before open":
				cancel()
			case "unsupported":
				provider.OpenMaintenance = nil
			case "disabled":
				s.Config.Auth.Enabled = false
			case "invalid limit":
				limit = 0
			}
			if err := s.RegisterAccountProvider("custom", provider); err != nil {
				t.Fatal(err)
			}

			count, err := s.CleanupAccountTokens(ctx, limit)
			wantOpen, wantDelete, wantCount := 1, 0, int64(0)
			switch scenario {
			case "success", "close failure":
				wantDelete, wantCount = 1, 3
			case "delete failure":
				wantDelete, wantCount = 1, 2
			case "canceled before open", "unsupported", "disabled", "invalid limit":
				wantOpen = 0
			}
			if opens != wantOpen || closes != wantOpen || deletes != wantDelete || count != wantCount {
				t.Fatalf("maintenance: opens=%d, closes=%d, deletes=%d, count=%d, error=%v", opens, closes, deletes, count, err)
			}
			wantError := scenario != "success" && scenario != "disabled"
			if (err != nil) != wantError {
				t.Fatalf("maintenance error = %v", err)
			}
			for expected, applies := range map[error]bool{
				openErr: scenario == "open failure", deleteErr: scenario == "delete failure",
				closeErr:         scenario == "open failure" || scenario == "delete failure" || scenario == "close failure",
				context.Canceled: scenario == "canceled before open" || scenario == "canceled after open",
			} {
				if applies && !errors.Is(err, expected) {
					t.Fatalf("lost error %v: %v", expected, err)
				}
			}
			if s.AccountRepository != nil || s.Config.Auth.AccountStorage.Options["connection"] != "custom-storage" {
				t.Fatal("maintenance changed runtime repository or configuration")
			}
			if err := s.Shutdown(); err != nil || closes != wantOpen {
				t.Fatal("shutdown repeated maintenance cleanup:", err)
			}
		})
	}
}

type maintainedAccount struct {
	user.AccountRepository
	user.TokenMaintenance
}

func TestAccountMaintenanceSuppliedRepository(t *testing.T) {
	s := accountProviderApplication("unregistered")
	s.AccountRepository = &suppliedAccountRepository{}
	if _, err := s.CleanupAccountTokens(context.Background(), 1); err == nil {
		t.Fatal("unsupported supplied repository silently skipped cleanup")
	}
	calls := 0
	s.AccountRepository = &maintainedAccount{TokenMaintenance: tokenMaintenanceFunc(func(context.Context, int) (int64, error) {
		calls++
		return 1, nil
	})}
	closed := false
	s.closeAccounts = func() error { closed = true; return nil }
	if count, err := s.CleanupAccountTokens(context.Background(), 1); count != 1 || err != nil || calls != 1 || closed {
		t.Fatalf("supplied repository: count=%d, error=%v, calls=%d, closed=%t", count, err, calls, closed)
	}
}

func TestAccountMaintenanceRetry(t *testing.T) {
	s := accountProviderApplication("custom")
	opens, closes := 0, 0
	openErr := errors.New("temporary open failure")
	if err := s.RegisterAccountProvider("custom", AccountProvider{
		Open: func(context.Context, map[string]string) (user.AccountRepository, func() error, error) {
			t.Fatal("maintenance called runtime initialization")
			return nil, nil, nil
		},
		OpenMaintenance: func(context.Context, map[string]string) (user.TokenMaintenance, func() error, error) {
			opens++
			if opens == 1 {
				return nil, func() error { closes++; return nil }, openErr
			}
			// Providers that borrow resources can omit a close function.
			return tokenMaintenanceFunc(func(context.Context, int) (int64, error) { return 1, nil }), nil, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if count, err := s.CleanupAccountTokens(context.Background(), 1); count != 0 || !errors.Is(err, openErr) || closes != 1 {
		t.Fatalf("failed attempt: count=%d, error=%v, closes=%d", count, err, closes)
	}
	for range 2 {
		if count, err := s.CleanupAccountTokens(context.Background(), 1); count != 1 || err != nil {
			t.Fatalf("subsequent batch: count=%d, error=%v", count, err)
		}
	}
	if opens != 3 || closes != 1 || s.AccountRepository != nil {
		t.Fatalf("maintenance retained stale state: opens=%d, closes=%d, repository=%v", opens, closes, s.AccountRepository)
	}
}

func TestAccountMaintenanceDeadline(t *testing.T) {
	for _, stage := range []string{"open", "delete"} {
		t.Run(stage, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := accountProviderApplication("custom")
				closes, deletes := 0, 0
				if err := s.RegisterAccountProvider("custom", AccountProvider{
					Open: func(context.Context, map[string]string) (user.AccountRepository, func() error, error) {
						t.Fatal("maintenance initialized runtime storage")
						return nil, nil, nil
					},
					OpenMaintenance: func(ctx context.Context, _ map[string]string) (user.TokenMaintenance, func() error, error) {
						if stage == "open" {
							<-ctx.Done()
						}
						return tokenMaintenanceFunc(func(ctx context.Context, _ int) (int64, error) {
							deletes++
							<-ctx.Done()
							return 0, ctx.Err()
						}), func() error { closes++; return nil }, nil
					},
				}); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				start := time.Now()
				count, err := s.CleanupAccountTokens(ctx, 1)
				wantDeletes := 0
				if stage == "delete" {
					wantDeletes = 1
				}
				if count != 0 || !errors.Is(err, context.DeadlineExceeded) || closes != 1 || deletes != wantDeletes || time.Since(start) != time.Second {
					t.Fatalf("deadline: count=%d, error=%v, closes=%d, deletes=%d, elapsed=%s", count, err, closes, deletes, time.Since(start))
				}
			})
		})
	}
}

func TestAccountMaintenanceBorrowsInitializedSQLitePool(t *testing.T) {
	s := accountProviderApplication("sqlite")
	s.Database = accountSetupTestDB(t)
	ctx := context.Background()
	if err := s.InitializeAccounts(ctx); err != nil {
		t.Fatal(err)
	}
	assertAccountRepositoryReady(t, s.AccountRepository)
	account, err := s.AccountRepository.GetByEmail(ctx, "ready@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AccountRepository.CreateToken(ctx, user.TokenMetadata{
		AccountID: account.ID, TokenHash: "expired-hash", Purpose: "auth-reset", ExpiresAt: time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	// Supplied storage must take precedence over configuration, without reopening it.
	s.Config.Auth.AccountStorage.Provider = "unregistered"
	if count, err := s.CleanupAccountTokens(ctx, 1); count != 1 || err != nil {
		t.Fatalf("borrowed cleanup: count=%d, error=%v", count, err)
	}
	if _, err := s.AccountRepository.GetToken(ctx, "stored-hash", "auth-reset"); err != nil {
		t.Fatal("maintenance closed the shared pool or deleted the live token:", err)
	}
	var missing *user.ErrTokenNotFound
	if _, err := s.AccountRepository.GetToken(ctx, "expired-hash", "auth-reset"); !errors.As(err, &missing) {
		t.Fatal("expired token remained:", err)
	}
	if err := s.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if err := s.Database.Ping(); err == nil {
		t.Fatal("application shutdown left the shared pool open")
	}
}
