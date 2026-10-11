package handlers_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/trentnix/hyperserver/pkg/handlers"
)

type closingModule struct {
	dependencyModule
	close func(context.Context) error
}

func (m *closingModule) Shutdown(ctx context.Context) error { return m.close(ctx) }

func TestInitializeRollsBackCompletedModules(t *testing.T) {
	for _, phase := range []string{"bind", "init", "publish", "cancel"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			setupErr, closeErr := errors.New("setup failed"), errors.New("close failed")
			var closed []string
			provider := &closingModule{close: func(ctx context.Context) error {
				if ctx.Err() != nil {
					t.Error("rollback inherited startup cancellation")
				}
				closed = append(closed, "provider")
				return closeErr
			}}
			provider.provide = func() map[string]any { return map[string]any{"storage": provider} }
			consumer := &closingModule{close: func(context.Context) error {
				closed = append(closed, "consumer")
				return nil
			}}
			consumer.bind = func(handlers.Dependencies) error {
				if phase == "bind" {
					return setupErr
				}
				return nil
			}
			consumer.init = func(context.Context) error {
				if phase == "init" {
					// Failed initialization owns its partial resource cleanup.
					closed = append(closed, "partial consumer")
					return setupErr
				}
				if phase == "cancel" {
					cancel()
				}
				return nil
			}
			consumer.provide = func() map[string]any { return nil }
			catalog := []handlers.Descriptor{
				{Name: "consumer", New: func() handlers.Handler { return consumer }, Provides: []string{"pages"}, Requires: []handlers.Requirement{{Capability: "storage"}}},
				{Name: "provider", New: func() handlers.Handler { return provider }, Provides: []string{"storage"}},
			}
			modules, err := handlers.Initialize(ctx, nil, catalog, nil)
			if modules != nil || !errors.Is(err, closeErr) || !strings.Contains(err.Error(), "consumer") {
				t.Fatalf("modules=%v error=%v", modules, err)
			}
			want := []string{"provider"}
			switch phase {
			case "bind", "init":
				if !errors.Is(err, setupErr) {
					t.Fatal("lost original error:", err)
				}
				if phase == "init" {
					want = []string{"partial consumer", "provider"}
				}
			case "publish", "cancel":
				want = []string{"consumer", "provider"}
				if phase == "cancel" && !errors.Is(err, context.Canceled) {
					t.Fatal("lost cancellation:", err)
				}
			}
			if !reflect.DeepEqual(closed, want) {
				t.Fatalf("cleanup=%v want=%v", closed, want)
			}
		})
	}
}

func TestCloseReverseOrderAndOnce(t *testing.T) {
	var closed []string
	firstErr, lastErr := errors.New("first close failed"), errors.New("last close failed")
	modules := []handlers.Instance{{Name: "no resources", Handler: &dependencyModule{}}}
	for _, name := range []string{"first", "last"} {
		modules = append(modules, handlers.Instance{Name: name, Handler: &closingModule{close: func(context.Context) error {
			closed = append(closed, name)
			if name == "first" {
				return firstErr
			}
			return lastErr
		}}})
	}
	err := handlers.Close(context.Background(), modules)
	if !errors.Is(err, firstErr) || !errors.Is(err, lastErr) || strings.Join(closed, ",") != "last,first" {
		t.Fatalf("cleanup=%v error=%v", closed, err)
	}
	if err := handlers.Close(context.Background(), modules); err != nil || len(closed) != 2 {
		t.Fatalf("repeated cleanup=%v error=%v", closed, err)
	}
}

func TestCloseDeadlineAndCancellation(t *testing.T) {
	for _, timeout := range []time.Duration{0, time.Second, time.Minute, -1} {
		t.Run(timeout.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx := context.Background()
				want := 10 * time.Second
				wantErr := context.DeadlineExceeded
				if timeout > 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, timeout)
					defer cancel()
					want = timeout
				} else if timeout < 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
					want, wantErr = 0, context.Canceled
				}
				var closed []string
				modules := []handlers.Instance{
					{Name: "provider", Handler: &closingModule{close: func(ctx context.Context) error {
						closed = append(closed, "provider")
						return ctx.Err()
					}}},
					{Name: "consumer", Handler: &closingModule{close: func(ctx context.Context) error {
						closed = append(closed, "consumer")
						<-ctx.Done()
						return nil // A late success must still report the timeout.
					}}},
				}
				start := time.Now()
				if err := handlers.Close(ctx, modules); !errors.Is(err, wantErr) {
					t.Fatal("cleanup error:", err)
				}
				if time.Since(start) != want || strings.Join(closed, ",") != "consumer,provider" {
					t.Fatalf("elapsed=%v cleanup=%v", time.Since(start), closed)
				}
			})
		})
	}
}

func TestRollbackHasFreshBoundedContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var cleanupContext context.Context
		module := &closingModule{close: func(ctx context.Context) error {
			cleanupContext = ctx
			if ctx.Err() != nil {
				t.Error("cleanup context already expired")
			}
			<-ctx.Done()
			return ctx.Err()
		}}
		module.init = func(ctx context.Context) error { <-ctx.Done(); return nil }
		start := time.Now()
		_, err := handlers.Initialize(context.Background(), nil, []handlers.Descriptor{dependencyDescriptor("slow", module)}, nil)
		if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) != 20*time.Second || cleanupContext == nil {
			t.Fatalf("error=%v elapsed=%v cleanup=%v", err, time.Since(start), cleanupContext)
		}
	})
}
