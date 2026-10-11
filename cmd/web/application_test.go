package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/database"
	"github.com/trentnix/hyperserver/pkg/server"
)

// A test driver exposes deadlines and connection cleanup without a remote database.
type startupDriver struct{ connection *startupConnection }

func (d startupDriver) Open(string) (driver.Conn, error) { return d.connection, nil }

type startupConnection struct {
	ping   func(context.Context) error
	closed bool
}

func (c *startupConnection) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unused") }
func (c *startupConnection) Begin() (driver.Tx, error)           { return nil, errors.New("unused") }
func (c *startupConnection) Ping(ctx context.Context) error      { return c.ping(ctx) }
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

func testApplicationWithPool(core *server.ApplicationServer, db *sqlx.DB) *application {
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

	app.Config.Database = config.DatabaseConfig{Driver: "sqlite3", Connection: ":memory:"}
	if err := app.initializeDatabase(t.Context()); err != nil {
		t.Fatal("retry:", err)
	}
	db := app.Database
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
	app, err := newApplication(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	// Supplying a pool without transferring ownership leaves cleanup with its owner.
	app.Database = db
	if err := app.initializeDatabase(t.Context()); err != nil || app.Database != db {
		t.Fatal("initialization replaced a supplied pool:", err)
	}
	if err := app.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		t.Fatal("shutdown closed a borrowed pool:", err)
	}
}
