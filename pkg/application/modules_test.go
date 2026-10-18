package application_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/application"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

type moduleStub struct {
	init         func(context.Context, *server.ApplicationServer) error
	routes       func(*routing.Routes) error
	shutdown     func(context.Context) error
	capabilities map[string]any
	bind         func(handlers.Dependencies) error
}

func (m *moduleStub) Init(ctx context.Context, app *server.ApplicationServer) error {
	if m.init != nil {
		return m.init(ctx, app)
	}
	return nil
}

func (m *moduleStub) Routes(routes *routing.Routes) error {
	if m.routes != nil {
		return m.routes(routes)
	}
	return nil
}

func (m *moduleStub) Shutdown(ctx context.Context) error {
	if m.shutdown != nil {
		return m.shutdown(ctx)
	}
	return nil
}

func (m *moduleStub) Capabilities() map[string]any { return m.capabilities }

func (m *moduleStub) BindDependencies(deps handlers.Dependencies) error {
	if m.bind != nil {
		return m.bind(deps)
	}
	return nil
}

func init() {
	// Test registration contains a factory, never a shared module instance.
	handlers.Register(handlers.Descriptor{Name: "application-test", New: func() handlers.Handler {
		return &moduleStub{routes: func(routes *routing.Routes) error {
			routes.HandleFunc("GET /registered", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
			return routes.Err()
		}}
	}})
}

func TestModuleCatalogMustBeSelectedExplicitly(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(map[bool]string{false: "no selection", true: "import catalog"}[explicit], func(t *testing.T) {
			options := application.Options{}
			if explicit {
				options.Modules = func(context.Context, *application.Application) ([]handlers.Descriptor, error) {
					return handlers.Registered(), nil
				}
			}
			app, err := application.New(t.Context(), config.Config{}, options)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { app.Shutdown() })
			w := httptest.NewRecorder()
			app.Web.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/registered", nil))
			want := http.StatusNotFound
			if explicit {
				want = http.StatusNoContent
			}
			if w.Code != want {
				t.Fatalf("status = %d, want %d", w.Code, want)
			}
		})
	}
}

func TestModulesUseSharedServicesAndExistingLifecycle(t *testing.T) {
	for _, stage := range []string{"success", "selection", "templates", "dependency", "init", "routes", "conflict", "canceled selection", "canceled init", "canceled routes"} {
		t.Run(stage, func(t *testing.T) {
			cfg := storageConfig(t)
			cfg.HTTP.Session.Types, cfg.HTTP.Session.Stores = nil, nil
			cfg.HTTP.DefaultRateLimit = config.RateLimitConfig{Enabled: true, Requests: 1, Window: time.Hour, MaxClients: 10}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			cause, closeErr := errors.New("startup failed"), errors.New("cleanup failed")
			var events []string
			var captured *application.Application
			accounts := &accountStub{}
			makeModule := func(name string) handlers.Descriptor {
				return handlers.Descriptor{Name: name, New: func() handlers.Handler {
					return &moduleStub{
						init: func(context.Context, *server.ApplicationServer) error {
							events = append(events, "init "+name)
							if name == "consumer" && stage == "init" {
								return cause // Failed Init owns its own partial cleanup.
							}
							if name == "consumer" && stage == "canceled init" {
								cancel()
							}
							return nil
						},
						capabilities: map[string]any{"storage": accounts},
						bind: func(deps handlers.Dependencies) error {
							if name == "consumer" {
								got, err := handlers.GetDependency[user.AccountRepository](deps, "storage")
								if err != nil || got != accounts {
									t.Fatalf("dependency = %v, err=%v", got, err)
								}
							}
							return nil
						},
						routes: func(routes *routing.Routes) error {
							events = append(events, "routes "+name)
							if name != "consumer" {
								return nil
							}
							routes.HandleFunc("GET /feature", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
							switch stage {
							case "routes":
								return cause
							case "conflict":
								routes.HandleFunc("GET /feature", func(http.ResponseWriter, *http.Request) {})
							case "canceled routes":
								cancel()
							}
							return routes.Err()
						},
						shutdown: func(ctx context.Context) error {
							if ctx.Err() != nil {
								t.Error("module cleanup inherited cancellation")
							}
							events = append(events, "close "+name)
							return closeErr
						},
					}
				}}
			}
			app, err := application.New(ctx, cfg, application.Options{
				AccountProviders: map[string]user.StorageProvider{"custom": {Open: func(context.Context, map[string]string) (user.AccountRepository, func() error, error) {
					events = append(events, "accounts")
					return accounts, func() error { events = append(events, "close accounts"); return closeErr }, nil
				}}},
				Modules: func(ctx context.Context, app *application.Application) ([]handlers.Descriptor, error) {
					captured = app
					if app.Accounts != accounts {
						t.Fatal("module selection ran without shared accounts")
					}
					events = append(events, "select")
					app.OnShutdown("callback resource", func(cleanupCtx context.Context) error {
						if cleanupCtx.Err() != nil {
							t.Error("cleanup inherited cancellation")
						}
						events = append(events, "close callback")
						return nil
					})
					if stage == "selection" {
						return nil, cause
					}
					if stage == "canceled selection" {
						cancel()
					}
					app.ContentManager.PrepareTemplates = func() error {
						events = append(events, "templates")
						if stage == "templates" {
							return cause
						}
						return nil
					}
					provider, consumer := makeModule("provider"), makeModule("consumer")
					provider.Provides = []string{"storage"}
					consumer.Requires = []handlers.Requirement{{Capability: "storage"}}
					if stage == "dependency" {
						return []handlers.Descriptor{consumer}, nil
					}
					return []handlers.Descriptor{consumer, provider}, nil // Resolver must reorder them.
				},
			})
			want := []string{"accounts", "select"}
			if stage != "selection" && stage != "canceled selection" {
				want = append(want, "templates")
			}
			switch stage {
			case "success", "routes", "conflict", "canceled routes":
				want = append(want, "init provider", "init consumer", "routes provider", "routes consumer", "close consumer", "close provider")
			case "init":
				want = append(want, "init provider", "init consumer", "close provider")
			case "canceled init":
				want = append(want, "init provider", "init consumer", "close consumer", "close provider")
			}
			want = append(want, "close callback", "close accounts")
			if stage == "success" {
				if err != nil {
					t.Fatal(err)
				}
				for i, status := range []int{http.StatusNoContent, http.StatusTooManyRequests} {
					w := httptest.NewRecorder()
					app.Web.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/feature", nil))
					if w.Code != status {
						t.Fatalf("request %d status = %d, want %d", i, w.Code, status)
					}
				}
				err = app.Shutdown()
			} else if app != nil || err == nil {
				t.Fatalf("failed setup returned app=%v, err=%v", app, err)
			}
			if !errors.Is(err, closeErr) {
				t.Fatal("lost cleanup error", err)
			}
			if stage == "selection" || stage == "templates" || stage == "init" || stage == "routes" {
				if !errors.Is(err, cause) {
					t.Fatal("lost startup error", err)
				}
			}
			if strings.HasPrefix(stage, "canceled") && !errors.Is(err, context.Canceled) {
				t.Fatal("lost cancellation", err)
			}
			if err := captured.Shutdown(); err != nil {
				t.Fatal("cleanup ran twice", err)
			}
			if !reflect.DeepEqual(events, want) {
				t.Fatalf("events = %v, want %v", events, want)
			}
		})
	}
}

func TestServicesAndModulesShareStartupDeadline(t *testing.T) {
	for _, budget := range []time.Duration{0, time.Minute} {
		t.Run(budget.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				cfg := storageConfig(t)
				cfg.HTTP.Session.Types, cfg.HTTP.Session.Stores = nil, nil
				want := 10 * time.Second
				if budget != 0 {
					cfg.App.InitializationTimeout = &budget
					want = budget
				}
				start := time.Now()
				var closed []string
				checkDeadline := func(ctx context.Context) {
					t.Helper()
					deadline, ok := ctx.Deadline()
					if !ok || !deadline.Equal(start.Add(want)) {
						t.Fatalf("deadline = %v, want %v", deadline, start.Add(want))
					}
				}
				app, err := application.New(t.Context(), cfg, application.Options{
					AccountProviders: map[string]user.StorageProvider{"custom": {Open: func(ctx context.Context, _ map[string]string) (user.AccountRepository, func() error, error) {
						checkDeadline(ctx)
						time.Sleep(want / 3)
						return &accountStub{}, func() error { closed = append(closed, "accounts"); return nil }, nil
					}}},
					Modules: func(ctx context.Context, app *application.Application) ([]handlers.Descriptor, error) {
						checkDeadline(ctx)
						time.Sleep(want / 3)
						return []handlers.Descriptor{{Name: "slow", New: func() handlers.Handler {
							return &moduleStub{
								init: func(ctx context.Context, _ *server.ApplicationServer) error {
									checkDeadline(ctx)
									<-ctx.Done()
									return nil // A late success must still be closed and rejected.
								},
								routes: func(*routing.Routes) error { t.Fatal("bound routes after expiration"); return nil },
								shutdown: func(ctx context.Context) error {
									if ctx.Err() != nil {
										t.Fatal("cleanup inherited expired context")
									}
									closed = append(closed, "module")
									return nil
								},
							}
						}}}, nil
					},
				})
				if app != nil || !errors.Is(err, context.DeadlineExceeded) || time.Since(start) != want {
					t.Fatalf("app=%v err=%v elapsed=%v", app, err, time.Since(start))
				}
				if !reflect.DeepEqual(closed, []string{"module", "accounts"}) {
					t.Fatal("cleanup order", closed)
				}
			})
		})
	}
}

type configurableModule struct {
	moduleStub
	settings struct{ Title string }
}

func TestModuleProviderSelection(t *testing.T) {
	for _, selected := range []bool{false, true} {
		t.Run(map[bool]string{false: "ambiguous", true: "explicit"}[selected], func(t *testing.T) {
			bound := false
			options := application.Options{Modules: func(context.Context, *application.Application) ([]handlers.Descriptor, error) {
				catalog := []handlers.Descriptor{{
					Name: "consumer", Requires: []handlers.Requirement{{Capability: "storage"}},
					New: func() handlers.Handler {
						return &moduleStub{bind: func(deps handlers.Dependencies) error {
							value, err := handlers.GetDependency[string](deps, "storage")
							if err != nil || value != "second" {
								t.Fatalf("dependency = %q, err=%v", value, err)
							}
							bound = true
							return nil
						}}
					},
				}}
				for _, name := range []string{"first", "second"} {
					catalog = append(catalog, handlers.Descriptor{
						Name: name, Provides: []string{"storage"},
						New: func() handlers.Handler { return &moduleStub{capabilities: map[string]any{"storage": name}} },
					})
				}
				return catalog, nil
			}}
			if selected {
				options.ProviderSelections = map[string]string{"storage": "second"}
			}
			app, err := application.New(t.Context(), config.Config{}, options)
			if selected {
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { app.Shutdown() })
				if !bound {
					t.Fatal("consumer did not receive selected provider")
				}
			} else if app != nil || err == nil || bound {
				t.Fatalf("ambiguous dependency accepted: app=%v err=%v bound=%v", app, err, bound)
			}
		})
	}
}

func (m *configurableModule) Configuration() any { return &m.settings }

func TestModuleConfigurationAndInstancesAreApplicationLocal(t *testing.T) {
	var instances []*configurableModule
	options := application.Options{Modules: func(context.Context, *application.Application) ([]handlers.Descriptor, error) {
		return []handlers.Descriptor{{Name: "feature", New: func() handlers.Handler {
			m := &configurableModule{}
			m.settings.Title = "default"
			instances = append(instances, m)
			return m
		}}}, nil
	}}
	for _, title := range []string{"configured", "default"} {
		cfg := config.Config{App: config.AppConfig{WorkingDirectory: t.TempDir()}}
		if title == "configured" {
			dir := filepath.Join(cfg.App.WorkingDirectory, "modules", "feature")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("title: configured\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		app, err := application.New(t.Context(), cfg, options)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { app.Shutdown() })
		if got := instances[len(instances)-1].settings.Title; got != title {
			t.Fatalf("title = %q, want %q", got, title)
		}
	}
	if instances[0] == instances[1] || instances[0].settings.Title != "configured" {
		t.Fatal("applications share module instances or configuration")
	}
}
