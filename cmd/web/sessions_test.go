package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/database"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/session"
)

func sessionTestApplication(t *testing.T) *application {
	t.Helper()
	db, err := database.Setup("sqlite3", filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	c := &config.Config{}
	c.HTTP.Session.JwtKey = "application-owned-session-test-key"
	c.HTTP.Session.TokenAge, c.HTTP.Session.CookieAge = time.Hour, time.Hour
	c.HTTP.Session.Types = map[string]string{"default": "sqliteStore"}
	c.HTTP.Session.Stores = map[string]map[string]string{"sqliteStore": {
		"enabled": "true", "connection": filepath.Join(t.TempDir(), "sessions.db"), "sessiontable": "sessions",
	}}
	s := testApplicationWithPool(&server.ApplicationServer{Config: c}, db)
	t.Cleanup(func() {
		if err := s.Shutdown(); err != nil {
			t.Error(err)
		}
	})
	return s
}

func TestApplicationOwnsSessionResources(t *testing.T) {
	first, second := sessionTestApplication(t), sessionTestApplication(t)
	for _, s := range []*application{first, second} {
		if err := initializeSessions(context.Background(), s); err != nil {
			t.Fatal(err)
		}
	}
	manager := first.SessionManager
	if err := initializeSessions(context.Background(), first); err != nil || first.SessionManager != manager {
		t.Fatal("repeated initialization replaced the manager:", err)
	}
	r := session.AddSessionManagerToRequestContext(httptest.NewRequest(http.MethodGet, "/", nil), first.SessionManager)
	value, err := session.New(r, "owned")
	if err != nil {
		t.Fatal(err)
	}
	if err := value.Save(httptest.NewRecorder(), r); err != nil {
		t.Fatal(err)
	}
	if err := first.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if err := value.Save(httptest.NewRecorder(), r); err == nil {
		t.Fatal("shutdown left the session pool open")
	}
	if err := first.Database.Ping(); err == nil {
		t.Fatal("shutdown left the application pool open")
	}
	r = session.AddSessionManagerToRequestContext(httptest.NewRequest(http.MethodGet, "/", nil), second.SessionManager)
	value, err = session.New(r, "independent")
	if err != nil {
		t.Fatal(err)
	}
	if err := value.Save(httptest.NewRecorder(), r); err != nil {
		t.Fatal("first application shutdown affected the second:", err)
	}
}

func TestApplicationSessionSetupCanRetry(t *testing.T) {
	s := sessionTestApplication(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := initializeSessions(ctx, s); !errors.Is(err, context.Canceled) || s.SessionManager != nil {
		t.Fatalf("canceled setup: %v, manager=%v", err, s.SessionManager)
	}
	if err := initializeSessions(context.Background(), s); err != nil {
		t.Fatal("retry after canceled setup:", err)
	}
}

func TestApplicationRejectsUnknownSessionProvider(t *testing.T) {
	app, err := newApplication(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	app.Config.HTTP.Session.Types = map[string]string{"default": "unknown"}
	if err := initializeSessions(t.Context(), app); err == nil || app.SessionManager != nil {
		t.Fatal("invalid session initialization succeeded")
	}
}

type ownedSessionStore struct {
	session.SessionStore
	close func() error
}

func (*ownedSessionStore) IsEnabled() bool { return true }
func (s *ownedSessionStore) Close() error  { return s.close() }

func testOwnedSessionManager(t *testing.T, close func() error) *session.SessionManager {
	t.Helper()
	manager, err := session.NewSessionManager(t.Context(), map[string]string{"default": "custom"}, map[string]session.StoreFactory{
		"custom": func(context.Context) (session.SessionStore, error) {
			return &ownedSessionStore{close: close}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.Close() })
	return manager
}

func TestSessionSetupPreservesBorrowedManager(t *testing.T) {
	app, err := newApplication(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	closes := 0
	manager := testOwnedSessionManager(t, func() error { closes++; return nil })
	app.SessionManager = manager
	app.Config.HTTP.Session.Types = map[string]string{"default": "unknown"}
	if err := initializeSessions(t.Context(), app); err != nil || app.SessionManager != manager {
		t.Fatalf("supplied manager was replaced or configuration was consulted: %v", err)
	}
	if err := app.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if closes != 0 {
		t.Fatal("application closed a borrowed session manager")
	}
	if err := manager.Close(); err != nil || closes != 1 {
		t.Fatalf("owner cleanup: closes=%d error=%v", closes, err)
	}
}

func TestOwnedSessionsFollowShutdownOrder(t *testing.T) {
	closeErr := errors.New("session cleanup failed")
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "canceled"}[canceled], func(t *testing.T) {
			app, err := newApplication(config.Config{})
			if err != nil {
				t.Fatal(err)
			}
			var order []string
			app.OnShutdown("shared pool", func(context.Context) error {
				order = append(order, "pool")
				return nil
			})
			manager := testOwnedSessionManager(t, func() error {
				order = append(order, "sessions")
				return closeErr
			})
			app.ownSessions(manager)
			if err := initializeSessions(t.Context(), app); err != nil {
				t.Fatal(err)
			}
			app.OnShutdown("consumer", func(context.Context) error {
				order = append(order, "consumer")
				return nil
			})

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if canceled {
				cancel()
			}
			err = app.ShutdownContext(ctx)
			if !errors.Is(err, closeErr) || errors.Is(err, context.Canceled) != canceled {
				t.Fatalf("shutdown error = %v", err)
			}
			if !slices.Equal(order, []string{"consumer", "sessions", "pool"}) {
				t.Fatalf("shutdown order = %v", order)
			}
			if err := app.Shutdown(); err != nil || len(order) != 3 {
				t.Fatalf("repeated cleanup: order=%v error=%v", order, err)
			}
		})
	}
}

func TestStartupFailureClosesOwnedSessions(t *testing.T) {
	app := sessionTestApplication(t)
	failure := errors.New("module setup failed")
	var active *session.Session
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	err := app.Run(t.Context(), func(ctx context.Context) (*http.Server, net.Listener, error) {
		if err := initializeSessions(ctx, app); err != nil {
			return nil, nil, err
		}
		request = session.AddSessionManagerToRequestContext(request, app.SessionManager)
		var err error
		active, err = session.New(request, "owned")
		if err != nil {
			return nil, nil, err
		}
		if err := active.Save(httptest.NewRecorder(), request); err != nil {
			return nil, nil, err
		}
		return nil, nil, failure
	})
	if !errors.Is(err, failure) {
		t.Fatalf("startup error = %v", err)
	}
	if err := active.Save(httptest.NewRecorder(), request); err == nil {
		t.Fatal("startup failure left the session pool open")
	}
	if err := app.Database.Ping(); err == nil {
		t.Fatal("startup failure left the application pool open")
	}
}
