package routing

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/trentnix/hyperserver/pkg/ratelimit"
)

func rateScope(t *testing.T, parent *Routes, requests int) *Routes {
	t.Helper()
	routes, err := parent.WithRateLimit(ratelimit.Policy{Requests: requests, Window: time.Minute, MaxClients: 10})
	if err != nil {
		t.Fatal(err)
	}
	return routes
}

func routeRequest(handler http.Handler, method, path, ip string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	r.RemoteAddr = ip + ":1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func noContent(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }

func TestRatePolicyPrecedenceAndIndependentCounters(t *testing.T) {
	mux := http.NewServeMux()
	root := NewRoutes(mux)
	app := rateScope(t, root, 20)
	module := rateScope(t, app, 10)
	busy := rateScope(t, module, 200)
	app.HandleFunc("GET /app", noContent)
	module.HandleFunc("GET /module", noContent)
	module.HandleFunc("GET /sibling", noContent)
	busy.HandleFunc("GET /busy", noContent)
	module.WithoutRateLimit().HandleFunc("GET /unlimited", noContent)
	root.HandleFunc("GET /root", noContent)
	rateScope(t, module.WithoutRateLimit(), 2).HandleFunc("GET /limited-again", noContent)

	for _, tc := range []struct {
		path      string
		allowance int
		unlimited bool
	}{
		{"/busy", 200, false}, {"/module", 10, false}, {"/sibling", 10, false},
		{"/app", 20, false}, {"/unlimited", 250, true}, {"/root", 250, true}, {"/limited-again", 2, false},
	} {
		t.Run(tc.path, func(t *testing.T) {
			for i := 0; i < tc.allowance; i++ {
				if w := routeRequest(mux, "GET", tc.path, "192.0.2.1"); w.Code != 204 {
					t.Fatalf("request %d status=%d", i+1, w.Code)
				}
			}
			if !tc.unlimited {
				if w := routeRequest(mux, "GET", tc.path, "192.0.2.1"); w.Code != 429 {
					t.Fatalf("overflow status=%d", w.Code)
				}
			}
		})
	}
}

func TestRatePolicySnapshotsAndValidation(t *testing.T) {
	mux := http.NewServeMux()
	policy := ratelimit.Policy{Requests: 1, Window: time.Minute, MaxClients: 1}
	root := NewRoutes(mux)
	scope, err := root.WithRateLimit(policy)
	if err != nil {
		t.Fatal(err)
	}
	policy.Requests = 100
	scope.HandleFunc("GET /first", noContent)
	child := rateScope(t, scope, 2)
	child.HandleFunc("GET /child", noContent)
	scope.HandleFunc("GET /later", noContent)
	for _, path := range []string{"/first", "/later"} {
		if w := routeRequest(mux, "GET", path, "192.0.2.1"); w.Code != 204 {
			t.Fatal(w.Code)
		}
		if w := routeRequest(mux, "GET", path, "192.0.2.1"); w.Code != 429 {
			t.Fatal("policy mutation or child scope changed parent")
		}
		if w := routeRequest(mux, "GET", path, "192.0.2.2"); w.Code != 429 {
			t.Fatal("per-route client capacity was ignored")
		}
	}
	if invalid, err := scope.WithRateLimit(ratelimit.Policy{}); err == nil || invalid != nil {
		t.Fatal("invalid scope accepted")
	}
	root.HandleFunc("GET /plain", noContent)
	for i := 0; i < 5; i++ {
		if w := routeRequest(mux, "GET", "/plain", "192.0.2.1"); w.Code != 204 {
			t.Fatal("child policy mutated root")
		}
	}
}

func TestRateCountersUseRegisteredPatterns(t *testing.T) {
	mux := http.NewServeMux()
	routes := rateScope(t, NewRoutes(mux), 1)
	routes.HandleFunc("GET /item/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") == "" {
			t.Error("route values were lost")
		}
		noContent(w, r)
	})
	routes.HandleFunc("POST /item/{id}", noContent)
	for _, tc := range []struct {
		method, path, ip string
		status           int
	}{
		{"GET", "/item/one", "192.0.2.1", 204},
		{"GET", "/item/two?key=another", "192.0.2.1", 429},
		{"HEAD", "/item/three", "192.0.2.1", 429},
		{"POST", "/item/one", "192.0.2.1", 204},
		{"GET", "/item/one", "192.0.2.2", 204},
		{"DELETE", "/item/one", "192.0.2.3", 405},
		{"GET", "/missing", "192.0.2.3", 404},
		{"GET", "/item/one", "192.0.2.3", 204},
	} {
		if w := routeRequest(mux, tc.method, tc.path, tc.ip); w.Code != tc.status {
			t.Fatalf("%s %s %s: %d, want %d", tc.method, tc.path, tc.ip, w.Code, tc.status)
		}
	}
}

func TestSharedBudgetSurvivesRouteOverrides(t *testing.T) {
	mux := http.NewServeMux()
	routes := rateScope(t, NewRoutes(mux), 1)
	shared, err := ratelimit.New("shared mail", ratelimit.Policy{Requests: 3, Window: time.Minute, MaxClients: 10})
	if err != nil {
		t.Fatal(err)
	}
	rateScope(t, routes, 200).Handle("POST /busy", shared.Handler(http.HandlerFunc(noContent)))
	routes.WithoutRateLimit().Handle("POST /unlimited", shared.Handler(http.HandlerFunc(noContent)))
	routes.HandleFunc("GET /unrelated", noContent)
	for _, path := range []string{"/busy", "/unlimited", "/busy"} {
		if w := routeRequest(mux, "POST", path, "192.0.2.1"); w.Code != 204 {
			t.Fatal("shared budget rejected too early")
		}
	}
	for _, path := range []string{"/busy", "/unlimited"} {
		if w := routeRequest(mux, "POST", path, "192.0.2.1"); w.Code != 429 {
			t.Fatal("override bypassed shared budget")
		}
	}
	if w := routeRequest(mux, "GET", "/unrelated", "192.0.2.1"); w.Code != 204 {
		t.Fatal("shared budget escaped its selected routes")
	}
}

func TestRatePolicyExpiresAndApplicationsAreIndependent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var muxes []*http.ServeMux
		for i := 0; i < 2; i++ {
			mux := http.NewServeMux()
			routes, err := NewRoutes(mux).WithRateLimit(ratelimit.Policy{Requests: 1, Window: time.Second, MaxClients: 1})
			if err != nil {
				t.Fatal(err)
			}
			routes.HandleFunc("GET /", noContent)
			muxes = append(muxes, mux)
		}
		for _, mux := range muxes {
			if w := routeRequest(mux, "GET", "/", "192.0.2.1"); w.Code != 204 {
				t.Fatal("applications share counters")
			}
			if w := routeRequest(mux, "GET", "/", "192.0.2.1"); w.Code != 429 {
				t.Fatal("limit not enforced")
			}
		}
		time.Sleep(time.Second)
		for _, mux := range muxes {
			if w := routeRequest(mux, "GET", "/", "192.0.2.2"); w.Code != 204 {
				t.Fatal("expiration did not free client capacity")
			}
		}
	})
}

func TestBodyAndRateLimitsCompose(t *testing.T) {
	for _, mode := range []string{"default body", "custom body", "unlimited body", "unlimited rate"} {
		t.Run(mode, func(t *testing.T) {
			mux := http.NewServeMux()
			routes := rateScope(t, NewRoutes(mux), 1)
			read := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, err := io.Copy(io.Discard, r.Body); err != nil {
					w.WriteHeader(413)
					return
				}
				w.WriteHeader(204)
			})
			var firstStatus int
			switch mode {
			case "default body":
				routes.Handle("POST /", read)
				firstStatus = 413
			case "custom body":
				routes.HandleWithBodyLimit("POST /", read, 2*DefaultMaxBodyBytes)
				firstStatus = 204
			case "unlimited body":
				routes.HandleWithoutBodyLimit("POST /", read)
				firstStatus = 204
			case "unlimited rate":
				routes.WithoutRateLimit().Handle("POST /", read)
				firstStatus = 413
			}
			for i := 0; i < 2; i++ {
				r := httptest.NewRequest("POST", "/", strings.NewReader(strings.Repeat("x", int(DefaultMaxBodyBytes)+1)))
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, r)
				want := firstStatus
				if i == 1 && mode != "unlimited rate" {
					want = 429
				}
				if w.Code != want {
					t.Fatalf("request %d: status=%d, want %d", i, w.Code, want)
				}
			}
		})
	}
}

func TestConcurrentRouteCounters(t *testing.T) {
	mux := http.NewServeMux()
	routes := rateScope(t, NewRoutes(mux), 10)
	var first, second atomic.Int64
	routes.HandleFunc("GET /first", func(w http.ResponseWriter, r *http.Request) { first.Add(1) })
	routes.HandleFunc("GET /second", func(w http.ResponseWriter, r *http.Request) { second.Add(1) })
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Go(func() {
			routeRequest(mux, "GET", "/first", "192.0.2.1")
			routeRequest(mux, "GET", "/second", "192.0.2.1")
		})
	}
	wg.Wait()
	if first.Load() != 10 || second.Load() != 10 {
		t.Fatalf("first=%d second=%d", first.Load(), second.Load())
	}
}

func TestRateLimitRegistrationDiagnostics(t *testing.T) {
	mux := http.NewServeMux()
	root := NewRoutes(mux)
	seen := make(map[string]int)
	root.OnRateLimitRegistered = func(pattern string, policy ratelimit.Policy) {
		if _, exists := seen[pattern]; exists {
			t.Errorf("duplicate notification for %s", pattern)
		}
		seen[pattern] = policy.Requests
		// Observers cannot change the policy retained by the scope or limiter.
		policy.Requests = 999
	}
	app := rateScope(t, root, 20)
	module := rateScope(t, app, 10)
	busy := rateScope(t, module, 200)
	if len(seen) != 0 {
		t.Fatal("creating a scope reported a route")
	}
	app.HandleFunc("GET /app", noContent)
	module.HandleWithBodyLimit("GET /module", http.HandlerFunc(noContent), 100)
	busy.HandleWithoutBodyLimit("GET /busy", http.HandlerFunc(noContent))
	module.WithoutRateLimit().HandleFunc("GET /unlimited", noContent)
	rateScope(t, module.WithoutRateLimit(), 1).HandleFunc("GET /limited-again", noContent)
	want := map[string]int{"GET /app": 20, "GET /module": 10, "GET /busy": 200, "GET /limited-again": 1}
	if len(seen) != len(want) {
		t.Fatalf("notifications = %v", seen)
	}
	for pattern, requests := range want {
		if seen[pattern] != requests {
			t.Errorf("%s reported %d, want %d", pattern, seen[pattern], requests)
		}
	}
	for i, status := range []int{204, 429} {
		if w := routeRequest(mux, "GET", "/limited-again", "192.0.2.1"); w.Code != status {
			t.Fatalf("request %d status=%d, want %d", i, w.Code, status)
		}
	}

	// A conflicting registration fails before emitting a diagnostic.
	busy.HandleFunc("GET /busy", noContent)
	if busy.Err() == nil || root.Err() != busy.Err() {
		t.Fatal("duplicate registration did not report a shared error")
	}
}
