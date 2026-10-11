package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
		if err := initializeSessions(context.Background(), s.ApplicationServer); err != nil {
			t.Fatal(err)
		}
	}
	manager := first.SessionManager
	if err := initializeSessions(context.Background(), first.ApplicationServer); err != nil || first.SessionManager != manager {
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
	if err := initializeSessions(ctx, s.ApplicationServer); !errors.Is(err, context.Canceled) || s.SessionManager != nil {
		t.Fatalf("canceled setup: %v, manager=%v", err, s.SessionManager)
	}
	if err := initializeSessions(context.Background(), s.ApplicationServer); err != nil {
		t.Fatal("retry after canceled setup:", err)
	}
}

func TestApplicationRejectsUnknownSessionProvider(t *testing.T) {
	app, err := newApplication(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	app.Config.HTTP.Session.Types = map[string]string{"default": "unknown"}
	if err := initializeSessions(t.Context(), app.ApplicationServer); err == nil || app.SessionManager != nil {
		t.Fatal("invalid session initialization succeeded")
	}
}
