package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/database"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
	"github.com/trentnix/hyperserver/pkg/services/session"
	sessionsqlite "github.com/trentnix/hyperserver/pkg/services/session/sqlite"
	"github.com/trentnix/hyperserver/pkg/services/user"
	usersqlite "github.com/trentnix/hyperserver/pkg/services/user/sqlite"
)

// A test driver exposes deadlines and connection cleanup without a remote database.
type startupDriver struct{ connection *startupConnection }

func (d startupDriver) Open(string) (driver.Conn, error) { return d.connection, nil }

type startupConnection struct {
	ping   func(context.Context) error
	closed bool
}

func (c *startupConnection) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unused") }

func (c *startupConnection) Begin() (driver.Tx, error) { return nil, errors.New("unused") }

func (c *startupConnection) Ping(ctx context.Context) error { return c.ping(ctx) }

func (c *startupConnection) Close() error {
	c.closed = true
	return nil
}

func TestDatabaseInitializationCleanup(t *testing.T) {
	want := errors.New("database unavailable")
	conn := &startupConnection{ping: func(ctx context.Context) error {
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 10*time.Second {
			t.Error("database setup has no bounded deadline")
		}
		return want
	}}
	name := t.TempDir()
	sql.Register(name, startupDriver{conn})
	app, err := newApplication(config.Config{Database: config.DatabaseConfig{Driver: name}})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.initializeDatabase(context.Background()); !errors.Is(err, want) || app.Database != nil || !conn.closed {
		t.Fatalf("failed setup: error=%v pool=%v closed=%t", err, app.Database, conn.closed)
	}
}

func TestDatabaseCancellationAfterPing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn := &startupConnection{ping: func(context.Context) error {
		cancel()
		return nil
	}}
	name := t.TempDir()
	sql.Register(name, startupDriver{conn})
	app, err := newApplication(config.Config{Database: config.DatabaseConfig{Driver: name}})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.initializeDatabase(ctx); !errors.Is(err, context.Canceled) || !conn.closed || app.Database != nil {
		t.Fatalf("cancellation after ping: error=%v closed=%t pool=%v", err, conn.closed, app.Database)
	}
}

func TestDatabaseInitializationCancellation(t *testing.T) {
	for _, duration := range []time.Duration{0, time.Second, 10 * time.Second} {
		t.Run(duration.String(), func(t *testing.T) {
			name := t.TempDir()
			synctest.Test(t, func(t *testing.T) {
				conn := &startupConnection{ping: func(ctx context.Context) error {
					<-ctx.Done()
					return ctx.Err()
				}}
				sql.Register(name, startupDriver{conn})
				app, err := newApplication(config.Config{Database: config.DatabaseConfig{Driver: name}})
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), duration)
				defer cancel()
				if duration == 10*time.Second {
					ctx = context.Background() // Exercise the initializer's own deadline.
				}
				started := time.Now()
				err = app.initializeDatabase(ctx)
				if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) != duration || app.Database != nil {
					t.Fatalf("canceled setup: duration=%s error=%v pool=%v", time.Since(started), err, app.Database)
				}
				if conn.closed != (duration > 0) {
					t.Fatal("setup opened a canceled connection or failed to close an acquired connection")
				}
			})
		})
	}
}

func TestShutdownHooksOrderOwnershipAndErrors(t *testing.T) {
	db, err := database.Setup("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	app := testApplicationWithPool(&server.ApplicationServer{}, db)
	other := &server.ApplicationServer{}
	var order []string
	moduleErr, accountErr := errors.New("module close failed"), errors.New("account close failed")
	accountsClosed := false
	app.OnShutdown("accounts", func(context.Context) error {
		order = append(order, "accounts")
		accountsClosed = true
		if err := app.Database.Ping(); err != nil {
			t.Error("account provider lost borrowed pool:", err)
		}
		return accountErr
	})
	for _, name := range []string{"provider", "consumer"} {
		app.OnShutdown(name, func(ctx context.Context) error {
			order = append(order, name)
			if ctx.Err() != nil {
				t.Error("unexpected canceled cleanup:", ctx.Err())
			}
			if err := app.Database.Ping(); err != nil || accountsClosed {
				t.Error("module lost shared services before cleanup")
			}
			return moduleErr
		})
	}
	app.OnShutdown("optional", nil)
	other.OnShutdown("other", func(context.Context) error { order = append(order, "other"); return nil })
	err = app.Shutdown()
	if !errors.Is(err, moduleErr) || !errors.Is(err, accountErr) || strings.Join(order, ",") != "consumer,provider,accounts" {
		t.Fatalf("order=%v error=%v", order, err)
	}
	if err := app.Database.Ping(); err == nil {
		t.Error("cleanup error left the pool open")
	}
	if err := app.Shutdown(); err != nil || len(order) != 3 {
		t.Fatalf("repeated cleanup: order=%v error=%v", order, err)
	}
	if err := other.Shutdown(); err != nil || strings.Join(order, ",") != "consumer,provider,accounts,other" {
		t.Fatalf("independent cleanup: order=%v error=%v", order, err)
	}
}

func testApplicationWithPool(core *server.ApplicationServer, db *sql.DB) *application {
	app := &application{ApplicationServer: core}
	if db != nil {
		app.ownDatabase(db)
	}
	return app
}

func TestApplicationDatabaseInitializationIsExplicitAndRetryable(t *testing.T) {
	app, err := newApplication(config.Config{Database: config.DatabaseConfig{Driver: "unknown"}})
	if err != nil {
		t.Fatal("unused database prevented construction:", err)
	}
	t.Cleanup(func() {
		if err := app.Shutdown(); err != nil {
			t.Error(err)
		}
	})
	if app.Database != nil {
		t.Fatal("construction opened a database")
	}
	if err := app.initializeDatabase(t.Context()); err == nil || app.Database != nil {
		t.Fatal("invalid database initialization succeeded")
	}

	app.Config.Database = config.DatabaseConfig{Driver: "sqlite3", Connection: ":memory:?_fk=false"}
	if err := app.initializeDatabase(t.Context()); err == nil || app.Database != nil {
		t.Fatal("disabled foreign keys accepted")
	}

	app.Config.Database = config.DatabaseConfig{Driver: "sqlite3", Connection: ":memory:"}
	if err := app.initializeDatabase(t.Context()); err != nil {
		t.Fatal("retry:", err)
	}
	db := app.Database
	if db.Stats().MaxOpenConnections != 1 {
		t.Fatal("SQLite pool must limit open connections")
	}
	for pragma, want := range map[string]int{"foreign_keys": 1, "busy_timeout": 5000} {
		var got int
		if err := db.QueryRowContext(t.Context(), "PRAGMA "+pragma).Scan(&got); err != nil || got != want {
			t.Fatalf("%s = %d, %v, want %d", pragma, got, err, want)
		}
	}
	app.Config.Database.Driver = "unused-after-initialization"
	if err := app.initializeDatabase(t.Context()); err != nil || app.Database != db {
		t.Fatal("repeated initialization replaced the pool:", err)
	}

	if err := app.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err == nil {
		t.Fatal("shutdown left the pool open")
	}
}

func TestApplicationPreservesBorrowedPool(t *testing.T) {
	db := shutdownTestDB(t)
	db.SetMaxOpenConns(2)
	if _, err := db.ExecContext(t.Context(), "PRAGMA foreign_keys = OFF; PRAGMA busy_timeout = 75"); err != nil {
		t.Fatal(err)
	}
	app, err := newApplication(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	// Supplying a pool without transferring ownership leaves cleanup with its owner.
	app.Database = db
	app.Config.Database.Driver = "must-not-open"
	if err := app.initializeDatabase(t.Context()); err != nil || app.Database != db {
		t.Fatal("initialization replaced a supplied pool:", err)
	}
	if db.Stats().MaxOpenConnections != 2 {
		t.Fatal("initialization changed supplied connection limit")
	}
	for pragma, want := range map[string]int{"foreign_keys": 0, "busy_timeout": 75} {
		var got int
		if err := db.QueryRowContext(t.Context(), "PRAGMA "+pragma).Scan(&got); err != nil || got != want {
			t.Fatalf("initialization changed %s: %d, %v", pragma, got, err)
		}
	}
	if err := app.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		t.Fatal("shutdown closed a borrowed pool:", err)
	}
}

func TestMailInitializesExplicitly(t *testing.T) {
	cfg := config.Config{Database: config.DatabaseConfig{Driver: "unknown"}, Mail: config.MailConfig{Timeout: -time.Second}}
	cfg.HTTP.Session.Types = map[string]string{"default": "unknown"}
	app, err := newApplication(cfg)
	if err != nil {
		t.Fatal("unused services prevented construction:", err)
	}
	if err := initializeMail(app); err == nil || app.Mail != nil || app.mailHealth != nil {
		t.Fatal("invalid mail initialization succeeded")
	}
	app.Config.Mail.Timeout = 0
	app.Config.Mail.FromAddress = "test@example.invalid"
	if err := initializeMail(app); err != nil {
		t.Fatal("mail retry:", err)
	}
	if app.mailHealth == nil {
		t.Fatal("SMTP setup omitted its connectivity probe")
	}
	mail := app.Mail
	app.Config.Mail.Timeout = -time.Second
	if err := initializeMail(app); err != nil || app.Mail != mail {
		t.Fatal("repeated initialization replaced the client:", err)
	}
	if !errors.Is(mail.ValidateConfig(), messaging.ErrMailUnavailable) {
		t.Fatal("unconfigured mail reported available delivery")
	}
	if err := app.Shutdown(); err != nil {
		t.Fatal(err)
	}
}

func TestMailSetupPreservesReplacement(t *testing.T) {
	app, err := newApplication(config.Config{Mail: config.MailConfig{Timeout: -time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	sender := &fakeMailSender{}
	client, err := messaging.NewMailClientWithSender("replacement@example.invalid", sender)
	if err != nil {
		t.Fatal(err)
	}
	app.Mail = client
	probes := 0
	app.mailHealth = func(context.Context) error { probes++; return nil }
	if err := initializeMail(app); err != nil || app.Mail != client {
		t.Fatal("setup replaced the supplied client or used SMTP settings:", err)
	}
	if err := app.Mail.ValidateConfig(); err != nil {
		t.Fatal(err)
	}
	if len(sender.snapshot()) != 0 {
		t.Fatal("setup attempted mail delivery")
	}
	if probes != 0 {
		t.Fatal("setup ran the supplied readiness probe")
	}
	if err := app.mailHealth(t.Context()); err != nil || probes != 1 {
		t.Fatal("setup replaced the supplied readiness probe:", err)
	}

	if err := app.Mail.Compose().To("recipient@example.invalid").Body("Hello").Send(context.Background()); err != nil {
		t.Fatal(err)
	}
	sent := sender.snapshot()
	if len(sent) != 1 || sent[0].From != "replacement@example.invalid" || sent[0].To != "recipient@example.invalid" {
		t.Fatalf("replacement delivery: %+v", sent)
	}
}

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
    jwtKey: loaded-session-signing-key000000
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
  jwtKey: loaded-account-signing-key000000
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
	if err := initializeSessions(t.Context(), app); err != nil {
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
	if _, err := app.AccountRepository.GetByEmail(t.Context(), "absent@example.invalid"); !errors.Is(err, user.ErrUserNotFound) {
		t.Fatalf("account repository was not prepared: %v", err)
	}
	r := session.AddSessionManagerToRequestContext(httptest.NewRequest(http.MethodGet, "/", nil), app.SessionManager)
	s, err := session.New(r, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Store.(*sessionsqlite.SQLiteStore); !ok {
		t.Fatalf("session provider = %T, want SQLite", s.Store)
	}
	if err := s.Save(httptest.NewRecorder(), r); err != nil {
		t.Fatal("session store was not prepared:", err)
	}
	if err := app.Mail.ValidateConfig(); err != nil {
		t.Fatal("mail did not use the loaded settings:", err)
	}
}
