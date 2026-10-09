package handlers_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/trentnix/hyperserver/pkg/handlers"
)

func TestInitializeUsesOneDeadline(t *testing.T) {
	for _, test := range []struct {
		name     string
		deadline time.Duration
		want     time.Duration
	}{
		{name: "default", want: 10 * time.Second},
		{name: "shorter caller deadline", deadline: 2 * time.Second, want: 2 * time.Second},
		{name: "longer caller deadline", deadline: time.Minute, want: time.Minute},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				type contextKey struct{}
				ctx := context.WithValue(context.Background(), contextKey{}, "startup value")
				if test.deadline != 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, test.deadline)
					defer cancel()
				}
				started := time.Now()
				first := dependencyDescriptor("first", &dependencyModule{init: func(context.Context) error {
					time.Sleep(test.want / 3)
					return nil
				}})
				provider := dependencyDescriptor("storage", &dependencyModule{
					init: func(got context.Context) error {
						deadline, ok := got.Deadline()
						if !ok || !deadline.Equal(started.Add(test.want)) {
							t.Fatalf("deadline = %v, bounded = %t", deadline, ok)
						}
						if got.Value(contextKey{}) != "startup value" {
							t.Error("lost caller's context values")
						}
						<-got.Done()
						return got.Err()
					},
					provide: func() map[string]any { t.Error("published failed storage"); return nil },
				})
				provider.Provides = []string{"storage"}
				consumer := dependencyDescriptor("consumer", &dependencyModule{bind: func(handlers.Dependencies) error {
					t.Error("bound consumer after storage timeout")
					return nil
				}})
				consumer.Requires = []handlers.Requirement{{Capability: "storage"}}
				instances, err := handlers.Initialize(ctx, nil, []handlers.Descriptor{first, consumer, provider}, nil)
				if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), `module "storage"`) {
					t.Fatalf("startup error = %v", err)
				}
				if elapsed := time.Since(started); elapsed != test.want {
					t.Fatalf("elapsed = %v, want %v", elapsed, test.want)
				}
				if len(instances) != 1 || instances[0].Name != "first" {
					t.Fatalf("initialized modules = %v", instances)
				}
			})
		})
	}
}

func TestInitializeRejectsSuccessAfterDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		provider := dependencyDescriptor("storage", &dependencyModule{
			init:    func(ctx context.Context) error { <-ctx.Done(); return nil },
			provide: func() map[string]any { t.Error("published timed-out storage"); return nil },
		})
		provider.Provides = []string{"storage"}
		instances, err := handlers.Initialize(context.Background(), nil, []handlers.Descriptor{provider}, nil)
		if !errors.Is(err, context.DeadlineExceeded) || len(instances) != 1 {
			t.Fatalf("instances = %v, error = %v", instances, err)
		}
	})
}

func TestInitializeReleasesDefaultDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var setup context.Context
	module := dependencyDescriptor("storage", &dependencyModule{init: func(got context.Context) error { setup = got; return nil }})
	if _, err := handlers.Initialize(ctx, nil, []handlers.Descriptor{module}, nil); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(setup.Err(), context.Canceled) {
		t.Fatal("initialization left its deadline context active")
	}
	if ctx.Err() != nil {
		t.Fatal("initialization canceled its caller's context")
	}
}
