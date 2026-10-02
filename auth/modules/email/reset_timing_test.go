package auth

import (
	"context"
	"testing"
	"testing/synctest"
	"time"
)

func TestWaitForResetResponse(t *testing.T) {
	const minimum = 2 * time.Second
	for _, processing := range []time.Duration{0, time.Second, minimum, 3 * time.Second} {
		t.Run(processing.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				start := time.Now()
				time.Sleep(processing)
				if err := waitForResetResponse(context.Background(), start.Add(minimum)); err != nil {
					t.Fatal(err)
				}
				if elapsed, want := time.Since(start), max(processing, minimum); elapsed != want {
					t.Fatalf("elapsed = %v, want %v", elapsed, want)
				}
			})
		})
	}
}

func TestWaitForResetResponseCancellation(t *testing.T) {
	for _, delay := range []time.Duration{0, time.Second} {
		t.Run(delay.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if delay == 0 {
					cancel()
				} else {
					go func() { time.Sleep(delay); cancel() }()
				}
				start := time.Now()
				if err := waitForResetResponse(ctx, start.Add(2*time.Second)); err != context.Canceled {
					t.Fatalf("wait error = %v, want cancellation", err)
				}
				if time.Since(start) != delay {
					t.Fatal("cancellation did not stop the wait")
				}
			})
		})
	}
}
