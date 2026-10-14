package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/mattn/go-sqlite3"
)

func TestConnectionOptions(t *testing.T) {
	for _, tc := range []struct {
		connection string
		timeout    string
	}{
		{":memory:", "5000"},
		{"contacts.db?_timeout=75&_fk=true&_journal=WAL", "75"},
		{"file:contacts%20here.db?mode=rwc&_busy_timeout=0&_foreign_keys=ON", "0"},
		{"file:shared?mode=memory&cache=shared&_timeout=10&_busy_timeout=010&_fk=1&_foreign_keys=yes", "10"},
	} {
		t.Run(tc.connection, func(t *testing.T) {
			dsn, err := connectionString(tc.connection)
			if err != nil {
				t.Fatal(err)
			}
			path, query, _ := strings.Cut(dsn, "?")
			originalPath, originalQuery, _ := strings.Cut(tc.connection, "?")
			options, err := url.ParseQuery(query)
			if err != nil || path != originalPath || options.Get("_foreign_keys") != "on" || options.Get("_busy_timeout") != tc.timeout || options.Has("_fk") || options.Has("_timeout") {
				t.Fatalf("normalized connection = %q, %v", dsn, err)
			}
			original, err := url.ParseQuery(originalQuery)
			if err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"_journal", "mode", "cache"} {
				if options.Get(key) != original.Get(key) {
					t.Fatalf("changed unrelated option %q", key)
				}
			}
		})
	}
}

func TestRejectUnsafeOrAmbiguousOptions(t *testing.T) {
	for _, query := range []string{
		"_fk=false", "_foreign_keys=0", "_fk=", "_fk=invalid",
		"_fk=true&_foreign_keys=off", "_fk=true&_fk=true",
		"_timeout=-1", "_timeout=", "_timeout=invalid", "_timeout=2147483648",
		"_busy_timeout=1&_timeout=2", "_busy_timeout=1&_busy_timeout=1",
		"_timeout=%xx", "invalid;query",
	} {
		t.Run(query, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "accounts.db")
			db, err := Open(path + "?" + query)
			if db != nil {
				db.Close()
				t.Fatal("invalid configuration returned a pool")
			}
			if err == nil || strings.Contains(err.Error(), path) {
				t.Fatalf("configuration error missing or exposes connection: %v", err)
			}
		})
	}
	for _, connection := range []string{"", "?mode=memory", "   "} {
		if db, err := Open(connection); db != nil || err == nil {
			if db != nil {
				db.Close()
			}
			t.Fatalf("empty path returned %v, %v", db, err)
		}
	}
}

func openPool(t *testing.T, connection string) *sql.DB {
	t.Helper()
	db, err := Open(connection)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return db
}

func assertConnection(t *testing.T, conn *sql.Conn, timeout int) {
	t.Helper()
	for query, want := range map[string]int{"PRAGMA foreign_keys": 1, "PRAGMA busy_timeout": timeout} {
		var got int
		if err := conn.QueryRowContext(t.Context(), query).Scan(&got); err != nil || got != want {
			t.Fatalf("%s = %d, %v, want %d", query, got, err, want)
		}
	}
}

func TestPolicyOnNewAndReplacementConnections(t *testing.T) {
	db := openPool(t, filepath.Join(t.TempDir(), "settings.db")+"?_timeout=75")
	if db.Stats().MaxOpenConnections != 1 {
		t.Fatal("pool does not bound open connections")
	}
	first, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	assertConnection(t, first, 75)
	// A pool owner can explicitly change its limit. Every connection still gets
	// the same settings, rather than depending on a PRAGMA on the first one.
	db.SetMaxOpenConns(2)
	second, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	assertConnection(t, second, 75)
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := first.ExecContext(t.Context(), "PRAGMA foreign_keys=OFF; PRAGMA busy_timeout=1"); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	db.SetMaxIdleConns(0)
	if db.Stats().OpenConnections != 0 {
		t.Fatal("test did not discard the original connections")
	}
	db.SetMaxIdleConns(1)
	replacement, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Close()
	assertConnection(t, replacement, 75)
}

func TestMemoryPoolKeepsDataAndEnforcesForeignKeys(t *testing.T) {
	db := openPool(t, ":memory:")
	if _, err := db.ExecContext(t.Context(), `CREATE TABLE parent (id INTEGER PRIMARY KEY);
		CREATE TABLE child (parent_id INTEGER REFERENCES parent(id)); INSERT INTO parent VALUES (1)`); err != nil {
		t.Fatal(err)
	}
	if db.Stats().Idle != 1 {
		t.Fatal("pool must retain its private in-memory connection")
	}
	if _, err := db.ExecContext(t.Context(), "INSERT INTO child VALUES (1)"); err != nil {
		t.Fatal("connection did not retain in-memory data:", err)
	}
	_, err := db.ExecContext(t.Context(), "INSERT INTO child VALUES (2)")
	var constraint sqlite3.Error
	if !errors.As(err, &constraint) || constraint.ExtendedCode != sqlite3.ErrConstraintForeignKey {
		t.Fatalf("foreign-key violation = %v", err)
	}
}

func TestLockContentionAndCancellation(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "busy timeout", true: "request deadline"}[cancel], func(t *testing.T) {
			connection := filepath.Join(t.TempDir(), "locked.db") + "?_timeout=50"
			owner, contender := openPool(t, connection), openPool(t, connection)
			if _, err := owner.ExecContext(t.Context(), "CREATE TABLE changes (id INTEGER)"); err != nil {
				t.Fatal(err)
			}
			tx, err := owner.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if _, err := tx.ExecContext(t.Context(), "INSERT INTO changes VALUES (1)"); err != nil {
				t.Fatal(err)
			}

			ctx := t.Context()
			if cancel {
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, 10*time.Millisecond)
				defer stop()
			}
			_, err = contender.ExecContext(ctx, "INSERT INTO changes VALUES (2)")
			var busy sqlite3.Error
			if cancel {
				// The driver's busy handler can finish with SQLITE_BUSY despite
				// the interrupt sent when the context deadline expires.
				if ctx.Err() != context.DeadlineExceeded {
					t.Fatalf("request deadline did not expire: %v", ctx.Err())
				}
				if !errors.Is(err, context.DeadlineExceeded) && (!errors.As(err, &busy) || busy.Code != sqlite3.ErrBusy) {
					t.Fatalf("canceled contended write = %v", err)
				}
			} else if !errors.As(err, &busy) || busy.Code != sqlite3.ErrBusy {
				t.Fatalf("contended write = %v", err)
			}
			if err := tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			var count int
			if err := contender.QueryRowContext(t.Context(), "SELECT count(*) FROM changes").Scan(&count); err != nil || count != 0 {
				t.Fatalf("failed write persisted data: %d, %v", count, err)
			}
			if _, err := contender.ExecContext(t.Context(), "INSERT INTO changes VALUES (3)"); err != nil {
				t.Fatal("retry after releasing lock:", err)
			}
		})
	}
}

func TestCancellationWhileWaitingForPool(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		db := openPool(t, ":memory:")
		conn, err := db.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		result := make(chan error, 1)
		go func() { result <- db.PingContext(ctx) }()
		synctest.Wait()
		if db.Stats().WaitCount != 1 {
			cancel()
			<-result
			t.Fatal("request did not wait for the occupied connection")
		}
		cancel()
		if err := <-result; !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled pool wait = %v", err)
		}
	})
}
