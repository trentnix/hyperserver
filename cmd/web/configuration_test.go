package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/session/stores"
	"github.com/trentnix/hyperserver/pkg/services/user"
	usersqlite "github.com/trentnix/hyperserver/pkg/services/user/sqlite"
)

func TestOptionalServicesUseLoadedConfiguration(t *testing.T) {
	// Isolate the load from the developer's environment and configuration files.
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "HYPERSERVER_") {
			t.Setenv(name, "")
			if err := os.Unsetenv(name); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Chdir(t.TempDir())
	const settings = `app:
  name: file-name
database:
  driver: sqlite3
  connection: ":memory:"
http:
  session:
    jwtKey: loaded-session-signing-key
    tokenAge: 1h
    cookieAge: 1h
    types:
      default: sqliteStore
    stores:
      sqliteStore:
        enabled: true
        connection: ":memory:"
        sessionTable: loaded_sessions
auth:
  enabled: true
  jwtKey: loaded-account-signing-key
  verificationTokenExpiration: 1h
  resetTokenExpiration: 1h
mail:
  hostname: localhost
  port: 1025
  user: test-user
  password: test-password
  fromAddress: sender@example.invalid
`
	if err := os.WriteFile("config.yaml", []byte(settings), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HYPERSERVER_APP_NAME", "loaded-name")
	cfg, err := config.GetConfig()
	if err != nil {
		t.Fatal(err)
	}

	// Neither application construction nor service initialization may reload these.
	if err := os.WriteFile("config.yaml", []byte("invalid: ["), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HYPERSERVER_APP_NAME", "changed-name")
	t.Setenv("HYPERSERVER_DATABASE_DRIVER", "unknown")
	t.Setenv("HYPERSERVER_HTTP_SESSION_TYPES_DEFAULT", "unknown")
	t.Setenv("HYPERSERVER_AUTH_ACCOUNTSTORAGE_PROVIDER", "unknown")
	t.Setenv("HYPERSERVER_MAIL_FROMADDRESS", "invalid")
	t.Setenv("HYPERSERVER_MAIL_TIMEOUT", "-1s")
	app, err := newApplication(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Shutdown(); err != nil {
			t.Error(err)
		}
	})
	if err := app.initializeDatabase(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := newAccountSetup(app).Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := initializeSessions(t.Context(), app.ApplicationServer); err != nil {
		t.Fatal(err)
	}
	if err := initializeMail(app); err != nil {
		t.Fatal(err)
	}

	if app.ContentManager.AppName != "loaded-name" {
		t.Fatal("rendering did not use the loaded application name")
	}
	if _, ok := app.AccountRepository.(*usersqlite.AccountRepository); !ok {
		t.Fatalf("account provider = %T, want SQLite", app.AccountRepository)
	}
	var notFound *user.ErrUserNotFound
	if _, err := app.AccountRepository.GetByEmail(t.Context(), "absent@example.invalid"); !errors.As(err, &notFound) {
		t.Fatalf("account repository was not prepared: %v", err)
	}
	r := session.AddSessionManagerToRequestContext(httptest.NewRequest(http.MethodGet, "/", nil), app.SessionManager)
	s, err := session.New(r, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Store.(*stores.SQLiteStore); !ok {
		t.Fatalf("session provider = %T, want SQLite", s.Store)
	}
	if err := s.Save(httptest.NewRecorder(), r); err != nil {
		t.Fatal("session store was not prepared:", err)
	}
	if err := app.Mail.ValidateConfig(); err != nil {
		t.Fatal("mail did not use the loaded settings:", err)
	}
}
