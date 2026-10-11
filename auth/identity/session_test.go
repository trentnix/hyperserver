package identity

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

type accountReaderStub struct {
	user.AccountRepository
	lookup func(context.Context, string) (*user.User, error)
}

func (s accountReaderStub) GetByID(ctx context.Context, id string) (*user.User, error) {
	return s.lookup(ctx, id)
}

func TestGetAuthenticatedUserUsesAccountRepository(t *testing.T) {
	manager := identityTestSessionManager(t, "cookieStore")
	setup := session.AddSessionManagerToRequestContext(httptest.NewRequest(http.MethodGet, "/", nil), manager)
	value, err := session.New(setup, userSessionKey)
	if err != nil {
		t.Fatal(err)
	}
	value.Data[userSessionKey], value.Data[userSessionVersionKey] = "account", int64(1)
	w := httptest.NewRecorder()
	if err := value.Save(w, setup); err != nil {
		t.Fatal(err)
	}

	for _, scenario := range []string{"success", "missing", "storage failure", "canceled", "revoked"} {
		t.Run(scenario, func(t *testing.T) {
			r := session.AddSessionManagerToRequestContext(httptest.NewRequest(http.MethodGet, "/", nil), manager)
			ctx, cancel := context.WithCancel(r.Context())
			defer cancel()
			r = r.WithContext(ctx)
			for _, cookie := range w.Result().Cookies() {
				r.AddCookie(cookie)
			}
			var wantErr error
			switch scenario {
			case "missing":
				wantErr = user.NewErrUserNotFound(nil)
			case "storage failure":
				wantErr = errors.New("storage unavailable")
			case "canceled":
				cancel()
				wantErr = context.Canceled
			}
			calls := 0
			reader := accountReaderStub{lookup: func(got context.Context, id string) (*user.User, error) {
				calls++
				if got != r.Context() || got.Err() != ctx.Err() || id != "account" {
					t.Fatal("identity lookup lost the request context or ID")
				}
				if wantErr != nil {
					return nil, wantErr
				}
				version := int64(1)
				if scenario == "revoked" {
					version++
				}
				return &user.User{ID: id, SessionVersion: version}, nil
			}}
			got, err := GetAuthenticatedUser(r, reader)
			if calls != 1 || !errors.Is(err, wantErr) || (got != nil) != (scenario == "success") {
				t.Fatalf("identity result = %v, %v, calls=%d", got, err, calls)
			}
		})
	}
}

func identityTestSessionManager(t *testing.T, store string) *session.SessionManager {
	t.Helper()
	cfg := &config.Config{}
	cfg.HTTP.Session.JwtKey = "account-reader-test-key"
	cfg.HTTP.Session.TokenAge, cfg.HTTP.Session.CookieAge = time.Hour, time.Hour
	cfg.HTTP.Session.Stores = map[string]map[string]string{
		"cookieStore": {"enabled": "true"},
		"sqliteStore": {"enabled": "true", "connection": filepath.Join(t.TempDir(), "sessions.db"), "sessiontable": "session"},
	}
	cfg.HTTP.Session.Types = map[string]string{"default": store}
	manager, err := session.NewSessionManager(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := manager.Close(); err != nil {
			t.Error(err)
		}
	})
	return manager
}

func TestGetAuthenticatedUserUsesRequestIdentity(t *testing.T) {
	want := &user.User{ID: "account", SessionVersion: 1}
	r := user.AddUserToRequestContext(httptest.NewRequest(http.MethodGet, "/", nil), want)
	got, err := GetAuthenticatedUser(r, nil)
	if err != nil || got != want {
		t.Fatalf("cached identity = %v, %v", got, err)
	}
}

func TestSetAuthenticatedUserFailurePreservesRequest(t *testing.T) {
	for _, tc := range []struct {
		name    string
		account *user.User
	}{
		{"nil account", nil},
		{"missing ID", &user.User{SessionVersion: 1}},
		{"missing version", &user.User{ID: "account"}},
		{"missing session manager", &user.User{ID: "account", SessionVersion: 1}},
		{"unsupported rotation", &user.User{ID: "account", SessionVersion: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := &user.User{ID: "original", SessionVersion: 1}
			r := user.AddUserToRequestContext(httptest.NewRequest(http.MethodGet, "/", nil), original)
			if tc.name == "unsupported rotation" {
				r = session.AddSessionManagerToRequestContext(r, identityTestSessionManager(t, "cookieStore"))
			}
			w := httptest.NewRecorder()
			if err := SetAuthenticatedUser(w, r, tc.account); err == nil {
				t.Fatal("invalid authentication setup succeeded")
			}
			if user.GetUserFromContext(r.Context()) != original || len(w.Result().Cookies()) != 0 {
				t.Fatal("failed authentication changed the request identity or cookies")
			}
		})
	}
}

func TestAuthenticatedSessionRoundTrip(t *testing.T) {
	manager := identityTestSessionManager(t, "sqliteStore")
	r := session.AddSessionManagerToRequestContext(httptest.NewRequest(http.MethodGet, "/", nil), manager)
	account := &user.User{ID: "account", SessionVersion: 3}
	w := httptest.NewRecorder()
	if err := SetAuthenticatedUser(w, r, account); err != nil {
		t.Fatal(err)
	}
	if user.GetUserFromContext(r.Context()) != account {
		t.Fatal("login did not update the request identity")
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "auth-user-session" {
		t.Fatalf("authentication cookies = %v", cookies)
	}

	next := session.AddSessionManagerToRequestContext(httptest.NewRequest(http.MethodGet, "/", nil), manager)
	next.AddCookie(cookies[0])
	stored, err := session.Get(next, "auth-user-session")
	if err != nil {
		t.Fatal(err)
	}
	var version int64
	if err := stored.DecodeValue("auth-session-version", &version); err != nil || version != account.SessionVersion || stored.Data["auth-user-session"] != account.ID {
		t.Fatalf("stored authentication data = %v, %v", stored.Data, err)
	}
	reader := accountReaderStub{lookup: func(ctx context.Context, id string) (*user.User, error) {
		if ctx != next.Context() || id != account.ID {
			t.Fatal("account lookup lost context or identity")
		}
		return account, nil
	}}
	got, err := GetAuthenticatedUser(next, reader)
	if err != nil || got != account {
		t.Fatalf("session identity = %v, %v", got, err)
	}

	next = user.AddUserToRequestContext(next, got)
	logout := httptest.NewRecorder()
	if err := LogoutAuthenticatedUser(logout, next); err != nil {
		t.Fatal(err)
	}
	if user.GetUserFromContext(next.Context()) != nil {
		t.Fatal("logout retained the request identity")
	}
	cookies = logout.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "auth-user-session" || cookies[0].MaxAge != -1 {
		t.Fatalf("logout cookies = %v", cookies)
	}
	stored, err = session.Get(next, userSessionKey)
	if err != nil || len(stored.Data) != 0 {
		t.Fatalf("logout retained cached session data: %v, %v", stored, err)
	}
	replay := session.AddSessionManagerToRequestContext(httptest.NewRequest(http.MethodGet, "/", nil), manager)
	replay.AddCookie(w.Result().Cookies()[0])
	got, err = GetAuthenticatedUser(replay, nil)
	if err != nil || got != nil {
		t.Fatalf("logged-out cookie restored identity: %v, %v", got, err)
	}
}

func TestClearAuthenticationCookieWithoutSessionManager(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "https://example.invalid/", nil)
	r.AddCookie(&http.Cookie{Name: "auth-user-session", Value: "invalid"})
	w := httptest.NewRecorder()
	ClearAuthenticationCookie(w, r)
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cleared cookies = %v", cookies)
	}
	cookie := cookies[0]
	if cookie.Name != "auth-user-session" || cookie.Value != "" || cookie.MaxAge != -1 || !cookie.HttpOnly || !cookie.Secure || cookie.Path != "/" || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("authentication cookie was not safely expired: %v", cookie)
	}
}
