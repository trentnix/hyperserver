package session_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	sessions "github.com/trentnix/hyperserver/pkg/services/session"
)

func TestSessionOperationsRejectUnknownStore(t *testing.T) {
	for name, operation := range map[string]func(*http.Request, string) (*sessions.Session, error){"Get": sessions.Get, "New": sessions.New} {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r = sessions.AddSessionManagerToRequestContext(r, &sessions.SessionManager{Types: map[string]string{"default": "missing"}})
			session, err := operation(r, "user")
			if session != nil || !errors.Is(err, sessions.ErrSessionStoreNotFound) {
				t.Fatalf("%s = %v, %v, want nil and ErrSessionStoreNotFound", name, session, err)
			}
		})
	}
}

func TestGetDoesNotCacheLoadFailures(t *testing.T) {
	cfg := validSessionConfig()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: "user", Value: "invalid.jwt.token"})
	manager, err := sessions.NewConfiguredManager(context.Background(), cfg, configuredTestProviders(cfg))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.Close() })
	r = sessions.AddSessionManagerToRequestContext(r, manager)
	for range 2 {
		s, err := sessions.Get(r, "user")
		if s != nil || err == nil {
			t.Fatalf("Get = %v, %v, want nil and a load error", s, err)
		}
	}

	// A later successful read can create and cache a session on the same request.
	r.Header.Del("Cookie")
	s, err := sessions.Get(r, "user")
	if err != nil || s == nil || !s.IsNew {
		t.Fatalf("Get after removing invalid cookie = %v, %v", s, err)
	}
	if cached, err := sessions.Get(r, "user"); err != nil || cached != s {
		t.Fatalf("successful load was not cached: %v", err)
	}
}
