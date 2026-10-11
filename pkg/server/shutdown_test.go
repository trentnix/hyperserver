package server

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestShutdownHooksOrderOwnershipAndErrors(t *testing.T) {
	app := &ApplicationServer{Database: accountSetupTestDB(t)}
	other := &ApplicationServer{}
	var order []string
	moduleErr, accountErr := errors.New("module close failed"), errors.New("account close failed")
	app.closeAccounts = func() error {
		order = append(order, "accounts")
		if err := app.Database.Ping(); err != nil {
			t.Error("account provider lost borrowed pool:", err)
		}
		return accountErr
	}
	for _, name := range []string{"provider", "consumer"} {
		app.OnShutdown(name, func(ctx context.Context) error {
			order = append(order, name)
			if ctx.Err() != nil {
				t.Error("unexpected canceled cleanup:", ctx.Err())
			}
			if err := app.Database.Ping(); err != nil || app.closeAccounts == nil {
				t.Error("module lost shared services before cleanup")
			}
			return moduleErr
		})
	}
	app.OnShutdown("optional", nil)
	other.OnShutdown("other", func(context.Context) error { order = append(order, "other"); return nil })
	err := app.Shutdown()
	if !errors.Is(err, moduleErr) || !errors.Is(err, accountErr) || strings.Join(order, ",") != "consumer,provider,accounts" {
		t.Fatalf("order=%v error=%v", order, err)
	}
	if err := app.Database.Ping(); err == nil {
		t.Error("cleanup error left the pool open")
	}
	if err := app.Shutdown(); err != nil || len(order) != 3 {
		t.Fatalf("repeated cleanup: order=%v error=%v", order, err)
	}
	if err := other.Shutdown(); err != nil || strings.Join(order, ",") != "consumer,provider,accounts,other" {
		t.Fatalf("independent cleanup: order=%v error=%v", order, err)
	}
}

func TestShutdownContextSharesDeadline(t *testing.T) {
	for _, timeout := range []time.Duration{0, time.Second, time.Minute} {
		t.Run(timeout.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				app := &ApplicationServer{}
				var order []string
				app.closeAccounts = func() error { order = append(order, "accounts"); return nil }
				app.OnShutdown("provider", func(ctx context.Context) error {
					order = append(order, "provider")
					return ctx.Err()
				})
				app.OnShutdown("consumer", func(ctx context.Context) error {
					order = append(order, "consumer")
					<-ctx.Done()
					return nil
				})
				ctx := context.Background()
				want := 10 * time.Second
				if timeout > 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, timeout)
					defer cancel()
					want = timeout
				}
				start := time.Now()
				err := app.ShutdownContext(ctx)
				if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) != want || strings.Join(order, ",") != "consumer,provider,accounts" {
					t.Fatalf("error=%v elapsed=%v order=%v", err, time.Since(start), order)
				}
			})
		})
	}
}
