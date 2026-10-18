package main

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/config"
	site "github.com/trentnix/hyperserver/modules/site"
	"github.com/trentnix/hyperserver/pkg/components/messages"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
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
			logs := &capturedLogs{}
			app.Config.Database.Connection = "private-connection-secret"
			err := SetupHandlers(context.Background(), app, newTestLogger(logs), catalog, selections)
			if mode != "selected" {
				if err == nil || len(steps) != 0 {
					t.Fatalf("invalid graph: error=%v steps=%v", err, steps)
				}
				if strings.Contains(logs.String(), "Modules initialized") {
					t.Fatal("failed initialization logged a success summary")
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
			for _, want := range []string{"Modules initialized", `modules="[two consumer one]"`, `capabilityProviders="[storage=two pages=consumer storage=one]"`, "providerSelections=map[storage:two]"} {
				if !strings.Contains(logs.String(), want) {
					t.Fatalf("startup summary missing %q: %s", want, logs.String())
				}
			}
			if strings.Contains(logs.String(), "private-connection-secret") {
				t.Fatal("startup summary logged configuration secrets")
			}
		})
	}
}

type registrationModule struct {
	bind        func(*routing.Routes)
	initialized bool
}

type multipleProviderModule struct {
	orderedModule
	selected *[]string
}

func (m *multipleProviderModule) Capabilities() map[string]any {
	if m.name == "two" {
		return map[string]any{"storage": m, "secondary": m}
	}
	return map[string]any{"storage": m}
}

func (m *multipleProviderModule) BindDependencies(d handlers.Dependencies) error {
	if m.name != "consumer" {
		return nil
	}
	providers, err := handlers.GetDependencies[*multipleProviderModule](d, "storage")
	if err != nil {
		return err
	}
	for _, provider := range providers {
		*m.selected = append(*m.selected, provider.name)
	}
	return nil
}

func TestStartupSummaryPreservesMultipleProviderOrder(t *testing.T) {
	var steps, selected []string
	factory := func(name string) func() handlers.Handler {
		return func() handlers.Handler {
			return &multipleProviderModule{orderedModule: orderedModule{name: name, steps: &steps}, selected: &selected}
		}
	}
	catalog := []handlers.Descriptor{
		{Name: "consumer", New: factory("consumer"), Requires: []handlers.Requirement{{Capability: "storage", Multiple: true}}},
		{Name: "one", New: factory("one"), Provides: []string{"storage"}, Requires: []handlers.Requirement{{Capability: "secondary"}}},
		{Name: "two", New: factory("two"), Provides: []string{"storage", "secondary"}},
	}
	app, err := server.NewApplicationServer(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Shutdown()
	logs := &capturedLogs{}
	if err := SetupHandlers(t.Context(), app, newTestLogger(logs), catalog, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Join(selected, ",") != "one,two" {
		t.Fatalf("binding order changed from catalog order: %v", selected)
	}
	if !strings.Contains(logs.String(), `modules="[two one consumer]"`) {
		t.Fatalf("summary omitted initialization order: %s", logs.String())
	}
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

func TestSetupHandlersStorageTimeoutPreventsRouteBinding(t *testing.T) {
	for _, test := range []struct {
		name                     string
		configured, parent, want time.Duration
	}{
		{name: "default", want: 10 * time.Second},
		{name: "shorter configured budget", configured: 2 * time.Second, want: 2 * time.Second},
		{name: "longer configured budget", configured: time.Minute, want: time.Minute},
		{name: "earlier parent", configured: time.Minute, parent: time.Second, want: time.Second},
		{name: "later parent", configured: time.Second, parent: time.Minute, want: time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				cfg := config.Config{}
				if test.configured != 0 {
					cfg.App.InitializationTimeout = &test.configured
				}
				app, err := server.NewApplicationServer(cfg)
				if err != nil {
					t.Fatal(err)
				}
				first := &registrationModule{bind: func(*routing.Routes) { t.Error("bound routes before storage was ready") }}
				catalog := []handlers.Descriptor{
					{Name: "first", New: func() handlers.Handler { return first }},
					site.Module(func(ctx context.Context) (site.ContactRepository, error) {
						<-ctx.Done()
						return nil, ctx.Err()
					}, nil),
				}
				started := time.Now()
				ctx := context.Background()
				if test.parent != 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, test.parent)
					defer cancel()
				}
				err = SetupHandlers(ctx, app, nil, catalog, nil)
				if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), `module "site"`) {
					t.Fatalf("storage setup error = %v", err)
				}
				if time.Since(started) != test.want || !first.initialized {
					t.Fatal("startup did not use one bounded initialization phase")
				}
				_, pattern := app.Web.Handler(httptest.NewRequest(http.MethodPost, "/contact", nil))
				if pattern != "" {
					t.Fatalf("failed site initialization registered %q", pattern)
				}
			})
		})
	}
}

func TestSetupHandlersRejectsInvalidInitializationTimeout(t *testing.T) {
	for _, timeout := range []time.Duration{0, -time.Second} {
		t.Run(timeout.String(), func(t *testing.T) {
			cfg := config.Config{App: config.AppConfig{InitializationTimeout: &timeout}}
			if err := cfg.Validate(); err == nil {
				t.Fatal("validation accepted nonpositive timeout")
			}
			app := &server.ApplicationServer{Config: &cfg, Web: http.NewServeMux()}
			catalog := []handlers.Descriptor{{Name: "test", New: func() handlers.Handler {
				t.Fatal("factory ran with invalid timeout")
				return nil
			}}}
			err := SetupHandlers(context.Background(), app, nil, catalog, nil)
			if err == nil || !strings.Contains(err.Error(), "app.initializationTimeout must be positive") {
				t.Fatalf("invalid timeout error = %v", err)
			}
		})
	}
}

func TestApplicationsOwnModulesAndAuthProviders(t *testing.T) {
	for _, secondAuth := range []bool{true, false} {
		t.Run(map[bool]string{true: "enabled", false: "disabled"}[secondAuth], func(t *testing.T) {
			t.Chdir(t.TempDir())
			first := newHTTPHarness(t, func(c *config.Config) {
				c.App.Name = "First application"
				c.HTTP.Session.Types["isolation-session"] = "sqliteStore"
			})
			second := newHTTPHarness(t, func(c *config.Config) {
				c.App.Name = "Second application"
				c.Auth.Enabled = secondAuth
				c.Auth.RegistrationEnabled = false
				c.HTTP.Session.Types["isolation-session"] = "sqliteStore"
			})
			if !secondAuth {
				if second.app.AccountRepository != nil {
					t.Fatal("disabled authentication initialized accounts")
				}
				w := second.request(http.MethodGet, "/auth/login/email", nil, false)
				if w.Code != http.StatusNotFound {
					t.Fatalf("disabled auth route returned %d", w.Code)
				}
			}
			for _, h := range []*httpHarness{first, second} {
				h.app.Web.HandleFunc("GET /isolation-session", func(w http.ResponseWriter, r *http.Request) {
					s, err := session.Get(r, "isolation-session")
					if err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					if s.IsNew {
						s.Data["owner"] = h.app.Config.App.Name
					}
					if err := s.Save(w, r); err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					fmt.Fprint(w, s.Data["owner"])
				})
			}

			var requests sync.WaitGroup
			for _, h := range []*httpHarness{first, second} {
				requests.Go(func() {
					for _, path := range []string{"/", "/isolation-session"} {
						w := h.request(http.MethodGet, path, nil, false)
						if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), h.app.Config.App.Name) {
							t.Errorf("application used another module's state at %s: %d %s", path, w.Code, w.Body.String())
						}
					}
				})
			}
			requests.Wait()
			first.assertRowCount(t, "session", 1)
			second.assertRowCount(t, "session", 1)
			// Even with matching test signing keys, separate storage must not accept
			// the other application's session record.
			base, err := url.Parse(first.baseURL)
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodGet, second.baseURL+"/isolation-session", nil)
			copied := false
			for _, cookie := range first.cookies.Cookies(base) {
				if cookie.Name == "isolation-session" {
					request.AddCookie(cookie)
					copied = true
				}
			}
			if !copied {
				t.Fatal("first application did not save its session cookie")
			}
			response := httptest.NewRecorder()
			second.handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK || response.Body.String() != second.app.Config.App.Name {
				t.Fatalf("second application accepted first session state: %d %s", response.Code, response.Body.String())
			}
			form := registrationForm()
			w := first.request(http.MethodPost, "/auth/register/email", form, true)
			if w.Code != 200 {
				t.Fatalf("first registration failed: %d %s", w.Code, w.Body.String())
			}
			first.assertRowCount(t, "user", 1)
			if secondAuth {
				second.assertRowCount(t, "user", 0)
			}
			if len(first.mail.snapshot()) != 1 || len(second.mail.snapshot()) != 0 {
				t.Fatal("registration used another application's mail client")
			}
			if err := second.app.Shutdown(); err != nil {
				t.Fatal(err)
			}
			if err := second.app.Database.Ping(); err == nil {
				t.Fatal("shutdown left the second database open")
			}
			w = first.request(http.MethodGet, "/isolation-session", nil, false)
			if w.Code != http.StatusOK || w.Body.String() != first.app.Config.App.Name {
				t.Fatalf("second shutdown affected first session: %d %s", w.Code, w.Body.String())
			}
			w = first.request(http.MethodPost, "/auth/login/email", url.Values{"email": form["email"], "password": form["password"]}, true)
			if w.Code != 200 || w.Header().Get("HX-Redirect") == "" {
				t.Fatalf("second shutdown affected first login: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

type lifecycleModule struct {
	orderedModule
	bindRoutes func(*routing.Routes) error
	close      func(context.Context) error
}

func (m *lifecycleModule) Routes(routes *routing.Routes) error {
	if m.bindRoutes != nil {
		return m.bindRoutes(routes)
	}
	return nil
}

func (m *lifecycleModule) Shutdown(ctx context.Context) error { return m.close(ctx) }

func TestSetupHandlersOwnsModuleCleanup(t *testing.T) {
	for _, mode := range []string{"success", "route error", "route conflict"} {
		t.Run(mode, func(t *testing.T) {
			app := testApplicationWithPool(&server.ApplicationServer{Config: &config.Config{}, Web: http.NewServeMux()}, shutdownTestDB(t))
			t.Cleanup(func() { _ = app.Shutdown() })
			var steps, closed []string
			routeErr, closeErr := errors.New("routes failed"), errors.New("cleanup failed")
			newModule := func(name string) *lifecycleModule {
				return &lifecycleModule{orderedModule: orderedModule{name: name, steps: &steps}, close: func(ctx context.Context) error {
					closed = append(closed, name)
					if ctx.Err() != nil {
						t.Error("module cleanup inherited canceled context:", ctx.Err())
					}
					if err := app.Database.Ping(); err != nil {
						t.Error("module lost shared pool:", err)
					}
					if name == "consumer" {
						return closeErr
					}
					return nil
				}}
			}
			provider, consumer := newModule("provider"), newModule("consumer")
			consumer.bindRoutes = func(routes *routing.Routes) error {
				if mode == "route error" {
					return routeErr
				}
				if mode == "route conflict" {
					routes.HandleFunc("GET /same", func(http.ResponseWriter, *http.Request) {})
					routes.HandleFunc("GET /same", func(http.ResponseWriter, *http.Request) {})
				}
				return nil // SetupHandlers must also inspect Routes.Err.
			}
			catalog := []handlers.Descriptor{
				{Name: "consumer", New: func() handlers.Handler { return consumer }, Requires: []handlers.Requirement{{Capability: "storage"}}},
				{Name: "provider", New: func() handlers.Handler { return provider }, Provides: []string{"storage"}},
			}
			ctx, cancel := context.WithCancel(context.Background())
			err := SetupHandlers(ctx, app.ApplicationServer, nil, catalog, nil)
			cancel()
			if mode == "success" {
				if err != nil || len(closed) != 0 {
					t.Fatalf("premature cleanup: %v %v", closed, err)
				}
				if err := app.Shutdown(); !errors.Is(err, closeErr) {
					t.Fatal("lost cleanup error:", err)
				}
			} else {
				if !errors.Is(err, closeErr) || !strings.Contains(err.Error(), "register module") {
					t.Fatal("startup did not include route and cleanup errors:", err)
				}
				if mode == "route error" && !errors.Is(err, routeErr) {
					t.Fatal("lost route error:", err)
				}
			}
			if strings.Join(closed, ",") != "consumer,provider" {
				t.Fatal("cleanup order:", closed)
			}
			if err := app.Shutdown(); err != nil || len(closed) != 2 {
				t.Fatalf("repeated cleanup=%v error=%v", closed, err)
			}
		})
	}
}

type setupAuthService struct {
	auth.AuthService
	initErr             error
	routeErr            error
	registerRoute       bool
	initialized, routed bool
}

type setupSessionStore struct {
	session.SessionStore
	session.RotatingStore
}

func (setupSessionStore) IsEnabled() bool { return true }

func (*setupAuthService) AuthType() string { return "custom" }

func (*setupAuthService) GetRegisterButton() template.HTML { return "" }

func (*setupAuthService) GetRegister(http.ResponseWriter, *http.Request) {}

func (*setupAuthService) Register(http.ResponseWriter, *http.Request) {}

func (s *setupAuthService) Init(*server.ApplicationServer) error {
	s.initialized = true
	return s.initErr
}

func (s *setupAuthService) Routes(routes *routing.Routes) error {
	if s.routeErr != nil {
		return s.routeErr
	}
	if s.registerRoute {
		routes.HandleFunc("GET /custom", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	}
	s.routed = true
	return nil
}

type verifyingSetupAuthService struct {
	*setupAuthService
	validationErr error
	validated     bool
}

func (s *verifyingSetupAuthService) ValidateVerification() error {
	s.validated = true
	return s.validationErr
}

func TestAuthModuleRequiresVerificationMechanism(t *testing.T) {
	for _, tc := range []struct {
		name                                string
		registration, verification, capable bool
		initErr, validationErr, routeErr    error
		warnRatePolicy                      bool
		want                                string
	}{
		{name: "missing mechanism", registration: true, verification: true, want: "requires a verification mechanism"},
		{name: "registration disabled", verification: true},
		{name: "verification optional", registration: true},
		{name: "configured mechanism", registration: true, verification: true, capable: true},
		{name: "unavailable mechanism", registration: true, verification: true, capable: true, validationErr: errors.New("delivery unavailable"), want: "delivery unavailable"},
		{name: "initialization failed", initErr: errors.New("provider failed"), want: "provider failed"},
		{name: "route registration failed", routeErr: errors.New("invalid route policy"), want: "invalid route policy"},
		{name: "route policy warning", warnRatePolicy: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := &setupAuthService{initErr: tc.initErr, routeErr: tc.routeErr, registerRoute: tc.warnRatePolicy}
			verifier := &verifyingSetupAuthService{setupAuthService: provider, validationErr: tc.validationErr}
			catalog := []auth.Descriptor{{Name: "custom", New: func() auth.AuthService {
				if tc.capable {
					return verifier
				}
				return provider
			}}}
			cfg := &config.Config{Auth: config.AuthConfig{
				Enabled: true, RegistrationEnabled: tc.registration, RegisterRequiresVerification: tc.verification,
				JwtKey: "test-auth-signing-key00000000000", VerificationTokenExpiration: time.Hour, ResetTokenExpiration: time.Hour,
				Services: map[string]map[string]string{"custom": {"enabled": "true"}},
			}}
			cfg.HTTP.Session.Types = map[string]string{"default": "sqliteStore"}
			if tc.warnRatePolicy {
				cfg.HTTP.DefaultRateLimit = config.RateLimitConfig{Enabled: true, Requests: 200, Window: time.Minute, MaxClients: 10}
				cfg.HTTP.SharedRateLimit = config.RateLimitConfig{Enabled: true, Requests: 20, Window: time.Minute, MaxClients: 10}
			}
			log := &capturedLogs{}
			app := &server.ApplicationServer{Config: cfg, Web: http.NewServeMux(), ContentManager: content.NewContentManager()}
			manager, err := session.NewSessionManager(t.Context(), map[string]string{"default": "custom"}, map[string]session.StoreFactory{
				"custom": func(context.Context) (session.SessionStore, error) { return setupSessionStore{}, nil },
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { manager.Close() })
			err = SetupHandlers(context.Background(), app, newTestLogger(log), []handlers.Descriptor{auth.Module(&struct{ user.AccountRepository }{}, manager, catalog)}, nil)
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) || provider.routed {
					t.Fatalf("error = %v, routed = %t, want %q without routes", err, provider.routed, tc.want)
				}
			} else if err != nil || !provider.routed {
				t.Fatalf("error = %v, routed = %t", err, provider.routed)
			}
			wantValidation := tc.registration && tc.verification && tc.capable && tc.initErr == nil
			if !provider.initialized || verifier.validated != wantValidation {
				t.Fatalf("initialized = %t, validated = %t", provider.initialized, verifier.validated)
			}
			if tc.warnRatePolicy {
				if !strings.Contains(log.String(), `level=WARN`) || !strings.Contains(log.String(), `route="GET /custom"`) {
					t.Fatalf("missing provider route warning: %s", log.String())
				}
			}
		})
	}
}

func TestApplicationSelectsNotifications(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		app, err := server.NewApplicationServer(config.Config{App: config.AppConfig{RenderNotifications: enabled}})
		if err != nil {
			t.Fatal(err)
		}
		if err := configureRendering(app); err != nil {
			t.Fatal(err)
		}
		loader := app.ContentManager.LoadNotifications
		if (loader != nil) != enabled {
			t.Fatalf("enabled=%t loader configured=%t", enabled, loader != nil)
		}
		if enabled {
			if _, _, err := loader(httptest.NewRequest(http.MethodGet, "/", nil)); err == nil {
				t.Fatal("session-backed notifications accepted missing sessions")
			}
		}

		called := false
		app.ContentManager.LoadNotifications = func(*http.Request) ([]messages.Notification, func(http.ResponseWriter) error, error) {
			called = true
			return nil, nil, nil
		}
		if err := configureRendering(app); err == nil {
			t.Fatal("duplicate setup accepted")
		}
		if _, _, err := app.ContentManager.LoadNotifications(httptest.NewRequest(http.MethodGet, "/", nil)); err != nil || !called {
			t.Fatal("duplicate setup replaced application override")
		}
	}
}
