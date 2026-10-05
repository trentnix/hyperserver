package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/ratelimit"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/logger"
)

func TestHTTPRateLimitedRoutes(t *testing.T) {
	for _, group := range []struct {
		name, exhaustPath string
		budget            int
		paths             []string
	}{
		{"auth", "/auth/login/email", 20, []string{
			"/auth/login/email", "/auth/register/email", "/auth/reset/request/email",
			"/auth/reset/email", "/auth/verify", "/auth/request/verify", "/auth/change/email",
		}},
		{"site", "/contact", 10, []string{"/contact", "/test-email"}},
	} {
		t.Run(group.name, func(t *testing.T) {
			runHTTPScenario(t, func(h *httpHarness) {
				account := h.seedUser(t, "existing@example.invalid")
				for _, path := range group.paths {
					// Every route inherits settings, not another route's used allowance.
					for i := 0; i < group.budget; i++ {
						r := httptest.NewRequest(http.MethodPost, h.baseURL+path, strings.NewReader("x=private%ZZ"))
						r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
						w := httptest.NewRecorder()
						h.handler.ServeHTTP(w, r)
						if w.Code == http.StatusTooManyRequests {
							t.Fatalf("%s request %d rejected early", path, i+1)
						}
					}
					for _, htmx := range []bool{false, true} {
						for _, email := range []string{account.Email, "unknown@example.invalid"} {
							w := h.request(http.MethodPost, path, url.Values{
								"email": {email}, "password": {"TestPassword1!"}, "passwordMatch": {"TestPassword1!"},
								"name": {"Test person"}, "message": {"Hello"},
							}, htmx)
							retry, err := strconv.Atoi(w.Header().Get("Retry-After"))
							if w.Code != 429 || w.Body.String() != "Too many requests. Please try again later.\n" || err != nil || retry < 1 || retry > 60 {
								t.Fatalf("%s HTMX=%t: status=%d retry=%q body=%q", path, htmx, w.Code, w.Header().Get("Retry-After"), w.Body.String())
							}
							if len(w.Result().Cookies()) != 0 || w.Header().Get("HX-Redirect") != "" || w.Header().Get("Location") != "" || w.Header().Get("Cache-Control") != "no-store" {
								t.Fatal("throttled response changed identity, redirected, or was cacheable")
							}
						}
					}
				}
				h.assertUserUnchanged(t, account)
				h.assertRowCount(t, "user", 1)
				h.assertRowCount(t, "usertoken", 0)
				h.assertRowCount(t, "hyperserver_contact_submission", 0)
				if len(h.mail.snapshot()) != 0 {
					t.Fatal("throttled request sent mail")
				}
				if !strings.Contains(h.logs.String(), "Request rate limited") || !strings.Contains(h.logs.String(), "route POST "+strings.ReplaceAll(group.exhaustPath, "/email", "/{authType}")) {
					t.Fatal("rejection did not identify its route budget")
				}

				// Displaying forms and signing out must remain available.
				if w := h.request(http.MethodGet, "/contact", nil, false); w.Code != 200 {
					t.Fatal("rate limit blocked a read route")
				}
				if w := h.request(http.MethodPost, "/auth/logout", nil, true); w.Code == 429 {
					t.Fatal("rate limit blocked logout")
				}
				// Another IP has an independent budget on the same route.
				r := httptest.NewRequest(http.MethodPost, h.baseURL+group.exhaustPath, strings.NewReader("x=private%ZZ"))
				r.RemoteAddr = "192.0.2.2:1000"
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				w := httptest.NewRecorder()
				h.handler.ServeHTTP(w, r)
				if w.Code != 400 {
					t.Fatalf("independent client status=%d", w.Code)
				}
			})
		})
	}
}

func TestHTTPGlobalAndRouteRateLimits(t *testing.T) {
	for _, htmx := range []bool{false, true} {
		t.Run(strconv.FormatBool(htmx), func(t *testing.T) {
			runHTTPScenario(t, func(h *httpHarness) {
				// Exhaust one route while leaving room in the shared application budget.
				for i := 0; i < 20; i++ {
					r := httptest.NewRequest(http.MethodPost, h.baseURL+"/auth/login/email", strings.NewReader("x=%ZZ"))
					r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					w := httptest.NewRecorder()
					h.handler.ServeHTTP(w, r)
					if w.Code != 400 {
						t.Fatalf("admitted request %d: status=%d", i+1, w.Code)
					}
				}
				if w := h.request(http.MethodPost, "/auth/login/email", nil, htmx); w.Code != 429 {
					t.Fatal("global budget bypassed the route budget")
				}
				if w := h.request(http.MethodGet, "/contact", nil, htmx); w.Code != 200 {
					t.Fatal("exhausted auth budget blocked an unrelated page")
				}
				// Assets consume the universal budget too, even when they do not exist.
				if w := h.request(http.MethodGet, "/css/no-such-file.css", nil, htmx); w.Code != 404 {
					t.Fatalf("asset status=%d, want 404 before the global budget is exhausted", w.Code)
				}
				for _, path := range []string{"/contact", "/css/no-such-file.css", "/auth/login"} {
					w := h.request(http.MethodGet, path, nil, htmx)
					if w.Code != 429 || w.Header().Get("Retry-After") == "" {
						t.Fatalf("%s escaped the global limit: %d", path, w.Code)
					}
				}
				// The contact route still has its full local budget, but cannot bypass the global limit.
				w := h.request(http.MethodPost, "/contact", url.Values{
					"name": {"Test person"}, "email": {"person@example.invalid"}, "message": {"Hello"},
				}, htmx)
				if w.Code != 429 {
					t.Fatalf("contact submission status=%d", w.Code)
				}
				h.assertRowCount(t, "hyperserver_contact_submission", 0)
				if len(h.mail.snapshot()) != 0 {
					t.Fatal("global rejection sent mail")
				}
				if !strings.Contains(h.logs.String(), "shared application") || !strings.Contains(h.logs.String(), "route POST /auth/login/{authType}") {
					t.Fatal("logs did not distinguish shared and route limits")
				}
			}, func(cfg *config.Config) {
				cfg.HTTP.SharedRateLimit = config.RateLimitConfig{Enabled: true, Requests: 23, Window: time.Minute, MaxClients: 100}
			})
		})
	}
}

func TestHTTPDefaultRateLimitInheritance(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		for _, path := range []string{"/contact", "/auth/login/email"} {
			if w := h.request(http.MethodGet, path, nil, false); w.Code != 200 {
				t.Fatalf("%s first status=%d", path, w.Code)
			}
			if w := h.request(http.MethodGet, path, nil, false); w.Code != 429 {
				t.Fatalf("%s default ignored: %d", path, w.Code)
			}
		}
		for i := 0; i < 2; i++ {
			w := h.request(http.MethodPost, "/contact", url.Values{"name": {"Test person"}, "email": {"person@example.invalid"}, "message": {"Hello"}}, false)
			if w.Code != 200 {
				t.Fatalf("module policy did not override application default: %d", w.Code)
			}
			w = h.request(http.MethodPost, "/auth/reset/request/email", url.Values{"email": {"unknown@example.invalid"}}, false)
			if w.Code != 200 {
				t.Fatalf("auth policy did not override application default: %d", w.Code)
			}
			if w = h.request(http.MethodPost, "/auth/logout", nil, true); w.Code == 429 {
				t.Fatal("explicit opt-out inherited a limit")
			}
		}
		h.assertRowCount(t, "hyperserver_contact_submission", 2)
	}, func(cfg *config.Config) {
		cfg.HTTP.DefaultRateLimit = config.RateLimitConfig{Enabled: true, Requests: 1, Window: time.Minute, MaxClients: 10}
	})
}

func TestApplicationRoutesRejectInvalidDefault(t *testing.T) {
	mux := http.NewServeMux()
	if routes, err := applicationRoutes(mux, config.HTTPConfig{DefaultRateLimit: config.RateLimitConfig{Enabled: true}}, nil); routes != nil || err == nil {
		t.Fatal("invalid default accepted")
	}
	if _, pattern := mux.Handler(httptest.NewRequest("GET", "/", nil)); pattern != "" {
		t.Fatal("failed setup registered a route")
	}
}

func TestGlobalRateLimitPrecedesSessionLoading(t *testing.T) {
	app := &server.ApplicationServer{Config: &config.Config{}, Web: http.NewServeMux()}
	app.Config.HTTP.SharedRateLimit = config.RateLimitConfig{Enabled: true, Requests: 1, Window: time.Minute, MaxClients: 10}
	logs := &capturedLogs{}
	handler, err := applicationHandler(app, &testLogger{logs: logs})
	if err != nil {
		t.Fatal(err)
	}
	// The first request is admitted and fails because there are no session services.
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != 500 {
		t.Fatalf("initial status=%d", w.Code)
	}

	body := &csrfUnreadBody{}
	r := httptest.NewRequest(http.MethodPost, "/", body)
	r.AddCookie(&http.Cookie{Name: "auth-user-session", Value: "invalid-token"})
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 429 || body.reads != 0 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("rejection touched request state: status=%d reads=%d cookies=%v", w.Code, body.reads, w.Result().Cookies())
	}
	if w.Header().Get("X-Request-ID") == "" || !strings.Contains(logs.String(), "Incoming request") {
		t.Fatal("rejection was not logged")
	}

	// A separately composed application must not inherit an exhausted budget.
	other, err := applicationHandler(app, &testLogger{logs: logs})
	if err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	other.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != 500 {
		t.Fatal("independent handler inherited the global limit")
	}
}

func TestApplicationHandlerRejectsInvalidRateLimit(t *testing.T) {
	for _, limit := range []config.RateLimitConfig{
		{Enabled: true, Window: time.Minute, MaxClients: 10},
		{Enabled: true, Requests: 1, MaxClients: 10},
		{Enabled: true, Requests: 1, Window: time.Minute},
	} {
		app := &server.ApplicationServer{Config: &config.Config{HTTP: config.HTTPConfig{SharedRateLimit: limit}}, Web: http.NewServeMux()}
		handler, err := applicationHandler(app, &testLogger{logs: &capturedLogs{}})
		if handler != nil || err == nil || !strings.Contains(err.Error(), "http.sharedRateLimit") {
			t.Fatalf("handler=%v error=%v", handler, err)
		}
	}
}

// Only Warn is implemented: diagnostics at any other level fail the test.
type ratePolicyWarningLogger struct {
	logger.Logger
	warnings []map[string]any
}

func (l *ratePolicyWarningLogger) Warn(message string, fields ...logger.Field) {
	entry := map[string]any{"message": message}
	for _, field := range fields {
		entry[field.Key] = field.Value
	}
	l.warnings = append(l.warnings, entry)
}

func TestApplicationRatePolicyWarnings(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		requests                   int
		window                     time.Duration
		defaultPolicy, optOut      bool
		disabled, noLogger, warned bool
	}{
		{name: "higher route", requests: 200, window: time.Minute, warned: true},
		{name: "higher default", requests: 200, window: time.Minute, defaultPolicy: true, warned: true},
		{name: "lower route replaces higher default", requests: 10, window: time.Minute},
		{name: "equal", requests: 20, window: time.Minute},
		{name: "longer window", requests: 200, window: time.Hour},
		{name: "shorter window", requests: 200, window: time.Second},
		{name: "opt out", requests: 200, window: time.Minute, optOut: true},
		{name: "shared disabled", requests: 200, window: time.Minute, disabled: true},
		{name: "no logger", requests: 200, window: time.Minute, noLogger: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.HTTPConfig{
				DefaultRateLimit: config.RateLimitConfig{Enabled: true, Requests: 200, Window: time.Minute, MaxClients: 10},
				SharedRateLimit:  config.RateLimitConfig{Enabled: !tc.disabled, Requests: 20, Window: time.Minute, MaxClients: 10},
			}
			capture := &ratePolicyWarningLogger{}
			var log logger.Logger = capture
			if tc.noLogger {
				log = nil
			}
			mux := http.NewServeMux()
			routes, err := applicationRoutes(mux, cfg, log)
			if err != nil {
				t.Fatal(err)
			}
			if !tc.defaultPolicy {
				routes, err = routes.WithRateLimit(ratelimit.Policy{Requests: tc.requests, Window: tc.window, MaxClients: 10})
				if err != nil {
					t.Fatal(err)
				}
			}
			if tc.optOut {
				routes = routes.WithoutRateLimit()
			}
			routes.HandleFunc("GET /items/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
			if !tc.warned {
				if len(capture.warnings) != 0 {
					t.Fatalf("unexpected warnings: %v", capture.warnings)
				}
				return
			}
			if len(capture.warnings) != 1 {
				t.Fatalf("warnings = %v, want one startup warning", capture.warnings)
			}
			entry := capture.warnings[0]
			if entry["route"] != "GET /items/{id}" || entry["routeRequests"] != 200 || entry["sharedRequests"] != 20 || entry["window"] != "1m0s" || !strings.Contains(entry["message"].(string), "per client IP") {
				t.Fatalf("incomplete warning: %v", entry)
			}
			for i := 0; i < 2; i++ {
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, httptest.NewRequest("GET", "/items/private-value", nil))
				if w.Code != 204 || len(capture.warnings) != 1 {
					t.Fatal("startup warning blocked the route or repeated during requests")
				}
			}
		})
	}
}

func TestHTTPRatePolicyStartupWarnings(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		// Both authentication and site modules must warn before the first request.
		logs := h.logs.String()
		if count := strings.Count(logs, "Confirm this restriction is intentional."); count != 9 {
			t.Fatalf("startup warning count = %d, want 7 auth and 2 site routes: %s", count, logs)
		}
		for _, route := range []string{"POST /auth/login/{authType}", "POST /auth/reset/request/{authType}", "POST /contact", "POST /test-email"} {
			if !strings.Contains(logs, route) {
				t.Errorf("missing startup warning for %s", route)
			}
		}
		// Warnings neither prevent startup nor change enforcement of the shared cap.
		for i := 0; i < 6; i++ {
			w := h.request(http.MethodPost, "/auth/login/email", nil, false)
			if (i < 5 && w.Code == 429) || (i == 5 && w.Code != 429) {
				t.Fatalf("request %d status = %d", i+1, w.Code)
			}
		}
		if count := strings.Count(h.logs.String(), "Confirm this restriction is intentional."); count != 9 {
			t.Fatal("requests repeated startup warnings")
		}
	}, func(cfg *config.Config) {
		cfg.HTTP.SharedRateLimit = config.RateLimitConfig{Enabled: true, Requests: 5, Window: time.Minute, MaxClients: 10}
	})
}

func TestHTTPModuleClientCapacities(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		for _, route := range []struct {
			path     string
			capacity int
		}{
			{"/auth/login/email", 2}, {"/auth/reset/request/email", 2},
			{"/contact", 3}, {"/test-email", 3},
		} {
			request := func(client int) *httptest.ResponseRecorder {
				r := httptest.NewRequest(http.MethodPost, h.baseURL+route.path, strings.NewReader("x=%ZZ"))
				r.RemoteAddr = "192.0.2." + strconv.Itoa(client) + ":1234"
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				w := httptest.NewRecorder()
				h.handler.ServeHTTP(w, r)
				return w
			}
			for client := 1; client <= route.capacity; client++ {
				if w := request(client); w.Code == 429 || w.Code == 500 {
					t.Fatalf("%s rejected client %d early: status=%d", route.path, client, w.Code)
				}
			}
			if w := request(route.capacity + 1); w.Code != 429 || w.Header().Get("Retry-After") == "" {
				t.Fatalf("%s exceeded configured client capacity: status=%d", route.path, w.Code)
			}
			if w := request(1); w.Code == 429 || w.Code == 500 {
				t.Fatalf("%s blocked an existing client with remaining allowance: status=%d", route.path, w.Code)
			}
		}
		h.assertRowCount(t, "user", 0)
		h.assertRowCount(t, "usertoken", 0)
		h.assertRowCount(t, "hyperserver_contact_submission", 0)
		if len(h.mail.snapshot()) != 0 || !strings.Contains(h.logs.String(), "client capacity reached") {
			t.Fatal("capacity check sent mail or failed to explain rejection")
		}
	}, func(cfg *config.Config) {
		cfg.Auth.RateLimit = &config.ModuleRateLimitConfig{MaxClients: 2}
		cfg.App.SiteRateLimit = &config.ModuleRateLimitConfig{MaxClients: 3}
		// Module capacities and request allowances must not inherit these smaller defaults.
		cfg.HTTP.DefaultRateLimit = config.RateLimitConfig{Enabled: true, Requests: 1, Window: time.Minute, MaxClients: 1}
	})
}
