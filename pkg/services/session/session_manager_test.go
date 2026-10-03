package session

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetStoreRejectsMissingSelection(t *testing.T) {
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
			store, err := NewSessionManager(cfg).getStore("user")
			var notFound *ErrSessionStoreNotFound
			if store != nil || !errors.As(err, &notFound) {
				t.Fatalf("getStore = %v, %v, want nil and ErrSessionStoreNotFound", store, err)
			}
		})
	}
}

func TestGetStoreSelectsCookieStore(t *testing.T) {
	for _, tc := range []struct {
		name  string
		types map[string]string
	}{
		{"named store takes precedence", map[string]string{"user": "cookieStore", "default": "missing"}},
		{"default fallback", map[string]string{"default": "cookieStore"}},
		{"case insensitive provider", map[string]string{"user": "COOKIESTORE"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validSessionConfig()
			cfg.HTTP.Session.Types = tc.types
			store, err := NewSessionManager(cfg).getStore("user")
			if err != nil {
				t.Fatal(err)
			}
			if cookie, ok := store.(*CookieStore); !ok || !cookie.IsEnabled() {
				t.Fatalf("getStore = %T, want enabled CookieStore", store)
			}
		})
	}
}

func TestGetStoreRejectsDisabledStore(t *testing.T) {
	cfg := validSessionConfig()
	cfg.HTTP.Session.Stores["cookiestore"]["enabled"] = "false"
	store, err := NewSessionManager(cfg).getStore("user")
	var disabled *ErrStoreDisabled
	if store != nil || !errors.As(err, &disabled) {
		t.Fatalf("getStore = %v, %v, want nil and ErrStoreDisabled", store, err)
	}
}

func TestSessionOperationsRejectUnknownStore(t *testing.T) {
	for name, operation := range map[string]func(*http.Request, string) (*Session, error){"Get": Get, "New": New} {
		t.Run(name, func(t *testing.T) {
			cfg := validSessionConfig()
			cfg.HTTP.Session.Types["default"] = "missing"
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r = AddSessionManagerToRequestContext(r, NewSessionManager(cfg))
			session, err := operation(r, "user")
			var notFound *ErrSessionStoreNotFound
			if session != nil || !errors.As(err, &notFound) {
				t.Fatalf("%s = %v, %v, want nil and ErrSessionStoreNotFound", name, session, err)
			}
		})
	}
}
