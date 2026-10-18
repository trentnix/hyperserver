package user

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/trentnix/hyperserver/config"
)

type tokenMaintenanceFunc func(context.Context, int) (int64, error)

func (f tokenMaintenanceFunc) DeleteExpiredTokens(ctx context.Context, limit int) (int64, error) {
	return f(ctx, limit)
}

func TestAccountMaintenanceLifecycle(t *testing.T) {
	openErr, deleteErr, closeErr := errors.New("open failed"), errors.New("delete failed"), errors.New("close failed")
	for _, scenario := range []string{"success", "open failure", "delete failure", "close failure", "nil maintenance", "canceled before open", "canceled after open", "unsupported", "invalid limit"} {
		t.Run(scenario, func(t *testing.T) {
			selection := config.AccountStorageConfig{Provider: "custom"}
			selection.Options = map[string]string{"connection": "custom-storage"}
			type contextKey struct{}
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), contextKey{}, "maintenance"))
			defer cancel()
			opens, deletes, closes := 0, 0, 0
			provider := StorageProvider{
				Open: func(context.Context, map[string]string) (AccountRepository, func() error, error) {
					t.Fatal("maintenance initialized runtime storage")
					return nil, nil, nil
				},
				OpenMaintenance: func(got context.Context, options map[string]string) (TokenMaintenance, func() error, error) {
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
			case "invalid limit":
				limit = 0
			}
			providers := map[string]StorageProvider{"custom": provider}

			count, err := CleanupConfiguredTokens(ctx, selection, providers, limit)
			wantOpen, wantDelete, wantCount := 1, 0, int64(0)
			switch scenario {
			case "success", "close failure":
				wantDelete, wantCount = 1, 3
			case "delete failure":
				wantDelete, wantCount = 1, 2
			case "canceled before open", "unsupported", "invalid limit":
				wantOpen = 0
			}
			if opens != wantOpen || closes != wantOpen || deletes != wantDelete || count != wantCount {
				t.Fatalf("maintenance: opens=%d, closes=%d, deletes=%d, count=%d, error=%v", opens, closes, deletes, count, err)
			}
			wantError := scenario != "success"
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
			if selection.Options["connection"] != "custom-storage" {
				t.Fatal("maintenance changed configuration")
			}
		})
	}
}

func TestAccountMaintenanceRetry(t *testing.T) {
	selection := config.AccountStorageConfig{Provider: "custom"}
	opens, closes := 0, 0
	openErr := errors.New("temporary open failure")
	providers := map[string]StorageProvider{"custom": {
		Open: func(context.Context, map[string]string) (AccountRepository, func() error, error) {
			t.Fatal("maintenance called runtime initialization")
			return nil, nil, nil
		},
		OpenMaintenance: func(context.Context, map[string]string) (TokenMaintenance, func() error, error) {
			opens++
			if opens == 1 {
				return nil, func() error { closes++; return nil }, openErr
			}
			// Providers that borrow resources can omit a close function.
			return tokenMaintenanceFunc(func(context.Context, int) (int64, error) { return 1, nil }), nil, nil
		},
	}}
	if count, err := CleanupConfiguredTokens(context.Background(), selection, providers, 1); count != 0 || !errors.Is(err, openErr) || closes != 1 {
		t.Fatalf("failed attempt: count=%d, error=%v, closes=%d", count, err, closes)
	}
	for range 2 {
		if count, err := CleanupConfiguredTokens(context.Background(), selection, providers, 1); count != 1 || err != nil {
			t.Fatalf("subsequent batch: count=%d, error=%v", count, err)
		}
	}
	if opens != 3 || closes != 1 {
		t.Fatalf("maintenance retained stale state: opens=%d, closes=%d", opens, closes)
	}
}

func TestAccountMaintenanceDeadline(t *testing.T) {
	for _, stage := range []string{"open", "delete"} {
		t.Run(stage, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				selection := config.AccountStorageConfig{Provider: "custom"}
				closes, deletes := 0, 0
				providers := map[string]StorageProvider{"custom": {
					Open: func(context.Context, map[string]string) (AccountRepository, func() error, error) {
						t.Fatal("maintenance initialized runtime storage")
						return nil, nil, nil
					},
					OpenMaintenance: func(ctx context.Context, _ map[string]string) (TokenMaintenance, func() error, error) {
						if stage == "open" {
							<-ctx.Done()
						}
						return tokenMaintenanceFunc(func(ctx context.Context, _ int) (int64, error) {
							deletes++
							<-ctx.Done()
							return 0, ctx.Err()
						}), func() error { closes++; return nil }, nil
					},
				}}
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				start := time.Now()
				count, err := CleanupConfiguredTokens(ctx, selection, providers, 1)
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
