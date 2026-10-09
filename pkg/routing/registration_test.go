package routing

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/pkg/ratelimit"
)

func TestRegistrationRejectsConflictingAndInvalidPatterns(t *testing.T) {
	for _, tc := range []struct {
		name, first, second string
	}{
		{"duplicate", "GET /items", "GET /items"},
		{"equivalent wildcards", "GET /items/{id}", "GET /items/{name}"},
		{"overlapping patterns", "GET /items/{id}", "GET /{kind}/latest"},
		{"method and path ambiguity", "GET /items/latest", "HEAD /items/{id}"},
		{"invalid wildcard", "GET /items", "GET /items/{"},
		{"invalid method", "GET /items", "G@T /other"},
		{"empty pattern", "GET /items", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			// Existing direct registrations must participate in conflict checks too.
			mux.HandleFunc(tc.first, noContent)
			routes := NewRoutes(mux)
			routes.HandleFunc(tc.second, noContent)
			if err := routes.Err(); err == nil || !strings.Contains(err.Error(), "register route") {
				t.Fatalf("registration error = %v", err)
			}
		})
	}
}

func TestRegistrationAllowsServeMuxSpecificity(t *testing.T) {
	mux := http.NewServeMux()
	routes := NewRoutes(mux)
	for _, pattern := range []string{"/", "GET /items/{id}", "GET /items/latest", "POST /items/{id}", "HEAD /items/latest", "GET example.com/items/{id}"} {
		routes.HandleFunc(pattern, func(w http.ResponseWriter, _ *http.Request) { w.Header().Set("Matched", pattern) })
	}
	if err := routes.Err(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ method, url, want string }{
		{"GET", "http://localhost/items/latest", "GET /items/latest"},
		{"GET", "http://localhost/items/123", "GET /items/{id}"},
		{"HEAD", "http://localhost/items/latest", "HEAD /items/latest"},
		{"POST", "http://localhost/items/123", "POST /items/{id}"},
		{"GET", "http://example.com/items/123", "GET example.com/items/{id}"},
		{"GET", "http://localhost/other", "/"},
	} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(tc.method, tc.url, nil))
		if got := w.Header().Get("Matched"); got != tc.want {
			t.Fatalf("%s %s matched %q, want %q", tc.method, tc.url, got, tc.want)
		}
	}
}

func TestRegistrationErrorsAreSharedAndSticky(t *testing.T) {
	mux := http.NewServeMux()
	root := NewRoutes(mux)
	limited := rateScope(t, root, 2)
	unlimited := limited.WithoutRateLimit()
	root.HandleFunc("GET /existing", noContent)
	limited.HandleFunc("GET /existing", noContent)
	first := root.Err()
	if first == nil || limited.Err() != first || unlimited.Err() != first {
		t.Fatal("child registration failure did not reach all scopes")
	}
	root.HandleFunc("GET /skipped", noContent)
	unlimited.HandleWithoutBodyLimit("GET /also-skipped", http.HandlerFunc(noContent))
	limited.HandleWithBodyLimit("", nil, 1)
	if root.Err() != first {
		t.Fatal("later registration replaced the first error")
	}
	for _, path := range []string{"/skipped", "/also-skipped"} {
		if _, pattern := mux.Handler(httptest.NewRequest("GET", path, nil)); pattern != "" {
			t.Fatalf("registered %q after failure", pattern)
		}
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/existing", nil))
	if w.Code != http.StatusNoContent {
		t.Fatal("failed registration replaced the original handler")
	}
	other := NewRoutes(http.NewServeMux())
	other.HandleFunc("GET /existing", noContent)
	if other.Err() != nil {
		t.Fatal("registration failure affected another application")
	}
}

func TestRegistrationRejectsNilHandlersBeforeWrapping(t *testing.T) {
	for _, register := range []func(*Routes, http.Handler){
		func(r *Routes, h http.Handler) { r.Handle("GET /test", h) },
		func(r *Routes, h http.Handler) { r.HandleWithBodyLimit("GET /test", h, 10) },
		func(r *Routes, h http.Handler) { r.HandleWithoutBodyLimit("GET /test", h) },
	} {
		for _, h := range []http.Handler{nil, http.HandlerFunc(nil)} {
			mux := http.NewServeMux()
			r := rateScope(t, NewRoutes(mux), 1)
			register(r, h)
			if r.Err() == nil || !strings.Contains(r.Err().Error(), "nil handler") {
				t.Fatalf("nil handler error = %v", r.Err())
			}
			if _, pattern := mux.Handler(httptest.NewRequest("GET", "/test", nil)); pattern != "" {
				t.Fatal("nil handler was registered")
			}
		}
	}
	r := NewRoutes(http.NewServeMux())
	r.HandleFunc("GET /test", nil)
	if r.Err() == nil {
		t.Fatal("nil HandleFunc was accepted")
	}
	if NewRoutes(nil).Err() == nil {
		t.Fatal("nil ServeMux was accepted")
	}
}

func TestRegistrationDoesNotRecoverUserPanics(t *testing.T) {
	for _, duringRequest := range []bool{false, true} {
		func() {
			want := "callback panic"
			if duringRequest {
				want = "handler panic"
			}
			defer func() {
				if got := recover(); got != want {
					t.Errorf("recovered %v, want %s", got, want)
				}
			}()
			mux := http.NewServeMux()
			r := rateScope(t, NewRoutes(mux), 1)
			if !duringRequest {
				r.OnRateLimitRegistered = func(string, ratelimit.Policy) { panic("callback panic") }
			}
			r.HandleFunc("GET /test", func(http.ResponseWriter, *http.Request) { panic("handler panic") })
			mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/test", nil))
		}()
	}
}
