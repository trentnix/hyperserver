package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
)

type routeFailureModule struct {
	initialized bool
	initErr     error
	routeErr    error
}

func (m *routeFailureModule) Init(context.Context, *server.ApplicationServer) error {
	m.initialized = true
	return m.initErr
}

func (m *routeFailureModule) Routes(*routing.Routes) error {
	return m.routeErr
}

func TestSetupHandlersRejectsInvalidRatePolicies(t *testing.T) {
	policyErr := errors.New("invalid module rate policy")

	for _, invalidDefault := range []bool{false, true} {
		t.Run(map[bool]string{false: "module", true: "application"}[invalidDefault], func(t *testing.T) {
			module := &routeFailureModule{routeErr: policyErr}
			catalog := []handlers.Descriptor{{Name: "test", New: func() handlers.Handler { return module }}}
			app := &server.ApplicationServer{Web: http.NewServeMux(), Config: &config.Config{}}
			app.Config.HTTP.DefaultRateLimit.Enabled = invalidDefault
			err := SetupHandlers(context.Background(), app, nil, catalog, nil)
			if invalidDefault {
				if err == nil || !strings.Contains(err.Error(), "http.defaultRateLimit") || module.initialized {
					t.Fatalf("invalid default: error=%v initialized=%t", err, module.initialized)
				}
			} else if !errors.Is(err, policyErr) || !strings.Contains(err.Error(), "routeFailureModule") || !module.initialized {
				t.Fatalf("module registration: error=%v initialized=%t", err, module.initialized)
			}
		})
	}
}

func TestSetupHandlersIdentifiesInitializationFailure(t *testing.T) {
	cause := errors.New("connection refused")
	module := &routeFailureModule{initErr: cause}
	catalog := []handlers.Descriptor{{Name: "test", New: func() handlers.Handler { return module }}}
	app := &server.ApplicationServer{Web: http.NewServeMux(), Config: &config.Config{}}
	err := SetupHandlers(context.Background(), app, nil, catalog, nil)
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), `initialize module "test"`) {
		t.Fatalf("initialization error = %v", err)
	}
}

type orderedModule struct {
	name  string
	steps *[]string
}

func (m *orderedModule) Init(context.Context, *server.ApplicationServer) error {
	*m.steps = append(*m.steps, "init "+m.name)
	return nil
}

func (m *orderedModule) Routes(*routing.Routes) error {
	*m.steps = append(*m.steps, "routes "+m.name)
	return nil
}

func (m *orderedModule) Capabilities() map[string]any {
	if m.name == "consumer" {
		return map[string]any{"pages": m}
	}
	return map[string]any{"storage": m}
}

func (m *orderedModule) BindDependencies(dependencies handlers.Dependencies) error {
	if m.name != "consumer" {
		return nil
	}
	_, err := handlers.GetDependency[*orderedModule](dependencies, "storage")
	return err
}

func TestSetupHandlersResolvesBeforeFactories(t *testing.T) {
	for _, mode := range []string{"missing", "ambiguous", "cycle", "selected"} {
		t.Run(mode, func(t *testing.T) {
			var steps []string
			factory := func(name string) func() handlers.Handler {
				return func() handlers.Handler {
					steps = append(steps, "new "+name)
					return &orderedModule{name: name, steps: &steps}
				}
			}
			catalog := []handlers.Descriptor{{Name: "consumer", New: factory("consumer"),
				Provides: []string{"pages"}, Requires: []handlers.Requirement{{Capability: "storage"}}}}
			var selections map[string]string
			if mode != "missing" {
				catalog = append(catalog, handlers.Descriptor{Name: "one", New: factory("one"), Provides: []string{"storage"}})
			}
			if mode == "ambiguous" || mode == "selected" {
				catalog = append(catalog, handlers.Descriptor{Name: "two", New: factory("two"), Provides: []string{"storage"}})
			}
			if mode == "cycle" {
				catalog[1].Requires = []handlers.Requirement{{Capability: "pages"}}
			}
			if mode == "selected" {
				selections = map[string]string{"storage": "two"}
			}
			app := &server.ApplicationServer{Config: &config.Config{}, Web: http.NewServeMux()}
			err := SetupHandlers(context.Background(), app, nil, catalog, selections)
			if mode != "selected" {
				if err == nil || len(steps) != 0 {
					t.Fatalf("invalid graph: error=%v steps=%v", err, steps)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := "new two,new consumer,new one,init two,init consumer,init one,routes two,routes consumer,routes one"
			if strings.Join(steps, ",") != want {
				t.Fatalf("initialization order=%v", steps)
			}
		})
	}
}

type registrationModule struct {
	bind        func(*routing.Routes)
	initialized bool
}

func (m *registrationModule) Init(context.Context, *server.ApplicationServer) error {
	m.initialized = true
	return nil
}

func (m *registrationModule) Routes(routes *routing.Routes) error {
	m.bind(routes)
	return nil // Startup must check Routes.Err even if a module forgets.
}

func TestSetupHandlersRejectsRouteConflicts(t *testing.T) {
	first := &registrationModule{bind: func(r *routing.Routes) {
		r.HandleFunc("GET /items/{id}", func(http.ResponseWriter, *http.Request) {})
	}}
	second := &registrationModule{bind: func(r *routing.Routes) {
		r.WithoutRateLimit().HandleFunc("GET /{kind}/latest", func(http.ResponseWriter, *http.Request) {})
	}}
	last := &registrationModule{bind: func(*routing.Routes) { t.Error("bound routes after startup failure") }}
	catalog := []handlers.Descriptor{
		{Name: "first", New: func() handlers.Handler { return first }},
		{Name: "conflicting", New: func() handlers.Handler { return second }},
		{Name: "last", New: func() handlers.Handler { return last }},
	}
	app := &server.ApplicationServer{Config: &config.Config{}, Web: http.NewServeMux()}
	err := SetupHandlers(context.Background(), app, nil, catalog, nil)
	if err == nil || !strings.Contains(err.Error(), `module "conflicting"`) || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("route-conflict error = %v", err)
	}
	if !last.initialized {
		t.Fatal("route binding began before all modules initialized")
	}
}

func TestSetupHandlersDoesNotRecoverModulePanics(t *testing.T) {
	defer func() {
		if got := recover(); got != "module bug" {
			t.Fatalf("recovered %v, want module bug", got)
		}
	}()
	module := &registrationModule{bind: func(*routing.Routes) { panic("module bug") }}
	app := &server.ApplicationServer{Config: &config.Config{}, Web: http.NewServeMux()}
	SetupHandlers(context.Background(), app, nil, []handlers.Descriptor{{Name: "bug", New: func() handlers.Handler { return module }}}, nil)
}

func TestSetupHandlersDoesNotBindRoutesOnInitializationFailure(t *testing.T) {
	cause := errors.New("storage unavailable")
	first := &registrationModule{bind: func(*routing.Routes) { t.Error("bound routes before initialization completed") }}
	failed := &routeFailureModule{initErr: cause}
	catalog := []handlers.Descriptor{
		{Name: "first", New: func() handlers.Handler { return first }},
		{Name: "failed", New: func() handlers.Handler { return failed }},
	}
	app := &server.ApplicationServer{Config: &config.Config{}, Web: http.NewServeMux()}
	if err := SetupHandlers(context.Background(), app, nil, catalog, nil); !errors.Is(err, cause) {
		t.Fatalf("initialization error = %v", err)
	}
	if !first.initialized {
		t.Fatal("first module did not initialize")
	}
}
