package session

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestManagerRejectsMissingSelection(t *testing.T) {
	for _, tc := range []struct {
		name  string
		types map[string]string
	}{
		{"unknown named store", map[string]string{"user": "missing", "default": "cookieStore"}},
		{"unknown default store", map[string]string{"default": "missing"}},
		{"empty named store", map[string]string{"user": "", "default": "cookieStore"}},
		{"empty default store", map[string]string{"default": ""}},
		{"missing default", map[string]string{"other": "cookieStore"}},
		{"no selections", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validSessionConfig()
			cfg.HTTP.Session.Types = tc.types
			manager, err := NewSessionManager(context.Background(), cfg)
			if manager != nil || err == nil {
				t.Fatalf("NewSessionManager = %v, %v, want nil and an error", manager, err)
			}
		})
	}
}

func TestGetStoreSelectsCookieStore(t *testing.T) {
	for _, tc := range []struct {
		name  string
		types map[string]string
	}{
		{"named store", map[string]string{"user": "cookieStore", "default": "cookieStore"}},
		{"default fallback", map[string]string{"default": "cookieStore"}},
		{"case insensitive provider", map[string]string{"user": "COOKIESTORE", "default": "cookieStore"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validSessionConfig()
			cfg.HTTP.Session.Types = tc.types
			manager, err := NewSessionManager(context.Background(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { manager.Close() })
			store, err := manager.getStore("user")
			if err != nil {
				t.Fatal(err)
			}
			if cookie, ok := store.(*CookieStore); !ok || !cookie.IsEnabled() {
				t.Fatalf("getStore = %T, want enabled CookieStore", store)
			}
		})
	}
}

func TestManagerRejectsDisabledStore(t *testing.T) {
	cfg := validSessionConfig()
	cfg.HTTP.Session.Stores["cookiestore"]["enabled"] = "false"
	manager, err := NewSessionManager(context.Background(), cfg)
	if manager != nil || err == nil {
		t.Fatalf("NewSessionManager = %v, %v, want nil and an error", manager, err)
	}
}

func TestSessionOperationsRejectUnknownStore(t *testing.T) {
	for name, operation := range map[string]func(*http.Request, string) (*Session, error){"Get": Get, "New": New} {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r = AddSessionManagerToRequestContext(r, &SessionManager{Types: map[string]string{"default": "missing"}})
			session, err := operation(r, "user")
			var notFound *ErrSessionStoreNotFound
			if session != nil || !errors.As(err, &notFound) {
				t.Fatalf("%s = %v, %v, want nil and ErrSessionStoreNotFound", name, session, err)
			}
		})
	}
}

func TestGetDoesNotCacheLoadFailures(t *testing.T) {
	cfg := validSessionConfig()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: "user", Value: "invalid.jwt.token"})
	manager, err := NewSessionManager(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.Close() })
	r = AddSessionManagerToRequestContext(r, manager)
	for range 2 {
		s, err := Get(r, "user")
		if s != nil || err == nil {
			t.Fatalf("Get = %v, %v, want nil and a load error", s, err)
		}
		if getCachedSession(r, "user") != nil {
			t.Fatal("failed load cached a replacement session")
		}
	}

	// A later successful read can create and cache a session on the same request.
	r.Header.Del("Cookie")
	s, err := Get(r, "user")
	if err != nil || s == nil || !s.IsNew {
		t.Fatalf("Get after removing invalid cookie = %v, %v", s, err)
	}
	if cached, err := Get(r, "user"); err != nil || cached != s {
		t.Fatalf("successful load was not cached: %v", err)
	}
}
