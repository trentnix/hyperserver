package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func registerTestPages(t *testing.T, manager *AuthManager) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	routes := routing.NewRoutes(mux)
	pages, err := manager.PageHandlers(routes)
	if err != nil {
		t.Fatal(err)
	}
	routes.Handle("GET /login", pages.Login)
	pages.Submissions.Handle("POST /login", pages.LoginSubmit)
	routes.Handle("GET /register", pages.Registration)
	pages.Submissions.Handle("POST /register", pages.RegistrationSubmit)
	if err := routes.Err(); err != nil {
		t.Fatal(err)
	}
	return mux
}

func TestPageHandlersProtectSiteRoutes(t *testing.T) {
	for _, hx := range []bool{false, true} {
		manager, account, sessions, _ := newLookupTestManager(t)
		manager.Enabled = true
		manager.registrationEnabled = true
		mux := registerTestPages(t, manager)
		for _, path := range []string{"/login", "/register"} {
			for _, method := range []string{"GET", "POST"} {
				r := httptest.NewRequest(method, path, nil)
				if hx {
					r.Header.Set("HX-Request", "true")
				}
				r = user.AddUserToRequestContext(r, account)
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, r)
				if w.Code != http.StatusForbidden || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("%s %s HTMX=%t: %d %v", method, path, hx, w.Code, w.Header())
				}
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest("PUT", path, nil))
			if w.Code != http.StatusMethodNotAllowed {
				t.Fatalf("PUT %s: %d", path, w.Code)
			}
			r := httptest.NewRequest("POST", path, strings.NewReader("provider="+strings.Repeat("x", int(routing.DefaultMaxBodyBytes))))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if hx {
				r.Header.Set("HX-Request", "true")
			}
			r = session.AddSessionManagerToRequestContext(r, sessions)
			w = httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != http.StatusRequestEntityTooLarge || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("oversized %s HTMX=%t: %d %v", path, hx, w.Code, w.Header())
			}
		}
	}
}

func TestPageHandlersLimitSubmissionsIndependently(t *testing.T) {
	manager, _, sessions, _ := newLookupTestManager(t)
	manager.Enabled, manager.registrationEnabled = true, true
	mux := registerTestPages(t, manager)
	for _, path := range []string{"/login", "/register"} {
		for attempt := 0; attempt <= 20; attempt++ {
			r := httptest.NewRequest("POST", path, strings.NewReader("provider=missing"))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r = session.AddSessionManagerToRequestContext(r, sessions)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			want := http.StatusNotFound
			if attempt == 20 {
				want = http.StatusTooManyRequests
				if w.Header().Get("Retry-After") == "" {
					t.Fatal("missing retry guidance")
				}
			}
			if w.Code != want || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("%s attempt %d: %d %v", path, attempt, w.Code, w.Header())
			}
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, session.AddSessionManagerToRequestContext(httptest.NewRequest("GET", path, nil), sessions))
		if w.Code == http.StatusTooManyRequests {
			t.Fatalf("submission limit blocked GET %s", path)
		}
	}
}

func TestPageHandlersRespectDisabledRegistration(t *testing.T) {
	manager := &AuthManager{Enabled: true, contentManager: content.NewContentManager()}
	mux := registerTestPages(t, manager)
	for _, method := range []string{"GET", "POST"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, "/register", nil))
		if w.Code != http.StatusForbidden || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s disabled registration: %d %v", method, w.Code, w.Header())
		}
	}
}

func TestPageHandlersRejectInvalidSetup(t *testing.T) {
	for _, manager := range []*AuthManager{
		{},
		{Enabled: true},
		{Enabled: true, contentManager: content.NewContentManager(), rateLimit: &config.ModuleRateLimitConfig{MaxClients: -1}},
	} {
		if _, err := manager.PageHandlers(routing.NewRoutes(http.NewServeMux())); err == nil {
			t.Fatal("invalid setup accepted")
		}
	}
}
