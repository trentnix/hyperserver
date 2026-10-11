package server

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestShutdownContextSharesDeadline(t *testing.T) {
	for _, timeout := range []time.Duration{0, time.Second, time.Minute} {
		t.Run(timeout.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				app := &ApplicationServer{}
				var order []string
				app.OnShutdown("accounts", func(context.Context) error { order = append(order, "accounts"); return nil })
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
