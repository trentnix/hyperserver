package session

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/database"
)

func sqliteManagerConfig(path, table string) *config.Config {
	c := validSessionConfig()
	c.HTTP.Session.Stores["sqliteStore"] = map[string]string{"enabled": "true", "connection": path, "sessiontable": table}
	c.HTTP.Session.Types["default"] = "sqliteStore"
	return c
}

func TestConcurrentColdSQLiteInitialization(t *testing.T) {
	c := sqliteManagerConfig(filepath.Join(t.TempDir(), "sessions.db"), "sessions")
	start := make(chan struct{})
	var workers sync.WaitGroup
	for range 16 {
		workers.Go(func() {
			<-start
			s, err := NewSQLiteStore(context.Background(), c)
			if err != nil {
				t.Error(err)
				return
			}
			defer s.Close()
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			value, err := s.New(r, "cold")
			if err != nil {
				t.Error(err)
				return
			}
			w := httptest.NewRecorder()
			if err := s.Save(w, r, value); err != nil {
				t.Error(err)
				return
			}
			r.AddCookie(w.Result().Cookies()[0])
			got, err := s.Get(r, "cold")
			if err != nil || got == nil || got.ID != value.ID || got.IsNew {
				t.Errorf("cold store round trip: %+v, %v", got, err)
			}
		})
	}
	close(start)
	workers.Wait()
}

func TestSessionManagerIsolationAndReuse(t *testing.T) {
	for _, sameFile := range []bool{false, true} {
		t.Run(fmt.Sprintf("same-file=%t", sameFile), func(t *testing.T) {
			root := t.TempDir()
			firstConfig := sqliteManagerConfig(filepath.Join(root, "first.db"), "first_sessions")
			firstConfig.HTTP.Session.Types["cookie"] = "cookieStore"
			firstConfig.HTTP.Session.Types["alias"] = "SQLITESTORE"
			secondConfig := sqliteManagerConfig(filepath.Join(root, "second.db"), "second_sessions")
			if sameFile {
				secondConfig.HTTP.Session.Stores["sqliteStore"]["connection"] = firstConfig.HTTP.Session.Stores["sqliteStore"]["connection"]
			}
			first, err := NewSessionManager(context.Background(), firstConfig)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { first.Close() })
			second, err := NewSessionManager(context.Background(), secondConfig)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { second.Close() })
			one, err := first.getStore("anything")
			if err != nil {
				t.Fatal(err)
			}
			two, err := second.getStore("anything")
			if err != nil {
				t.Fatal(err)
			}
			if one.(*SQLiteStore).db == two.(*SQLiteStore).db {
				t.Fatal("independent managers share a pool")
			}
			value, request := savedSession(t, one)
			if got, err := two.Get(request, value.Name); err != nil || !got.IsNew {
				t.Fatalf("second store read the first store's session: %+v, %v", got, err)
			}
			// Configuration changes after construction must not redirect the manager
			// or replace its signing key. Aliases reuse the prepared provider.
			firstConfig.HTTP.Session.Types["default"] = "cookieStore"
			firstConfig.HTTP.Session.Stores["sqliteStore"]["connection"] = "/missing/sessions.db"
			firstConfig.HTTP.Session.JwtKey = "changed"
			for _, name := range []string{"anything", "alias"} {
				if got, err := first.getStore(name); err != nil || got != one {
					t.Fatal("manager did not reuse its original provider")
				}
			}
			if got, err := first.getStore("cookie"); err != nil || got == one {
				t.Fatal("named provider did not override the default")
			}
			if got, err := one.Get(request, value.Name); err != nil || got.ID != value.ID {
				t.Fatal("configuration mutation changed the active signing key")
			}
			if err := first.Close(); err != nil {
				t.Fatal(err)
			}
			if err := one.(*SQLiteStore).db.Ping(); err == nil {
				t.Fatal("manager did not close its provider")
			}
			savedSession(t, two)
			if err := first.Close(); err != nil {
				t.Fatal("repeated Close failed:", err)
			}
		})
	}
}

func TestSessionInitializationFailureAndRetry(t *testing.T) {
	for _, failure := range []string{"open", "schema", "canceled", "deadline"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "sessions.db")
			c := sqliteManagerConfig(path, "sessions")
			ctx := context.Background()
			var want error
			switch failure {
			case "open":
				c.HTTP.Session.Stores["sqliteStore"]["connection"] = filepath.Join(root, "missing", "sessions.db")
			case "schema":
				db, err := database.Setup("sqlite3", path)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { db.Close() })
				// Force index creation to fail after the session table was created.
				if _, err := db.Exec("CREATE TABLE sessions_expiration (id TEXT)"); err != nil {
					t.Fatal(err)
				}
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				want = context.Canceled
			case "deadline":
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 0)
				defer cancel()
				want = context.DeadlineExceeded
			}
			manager, err := NewSessionManager(ctx, c)
			if manager != nil || err == nil || (want != nil && !errors.Is(err, want)) {
				t.Fatalf("failed setup returned %v, %v", manager, err)
			}
			if failure == "schema" {
				db, err := database.Setup("sqlite3", path)
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				if exists, err := database.TableExists(db.DB, "sessions"); err != nil || exists {
					t.Fatal("failed setup left a partial schema:", err)
				}
				if _, err := db.Exec("DROP TABLE sessions_expiration"); err != nil {
					t.Fatal(err)
				}
			} else if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("failed setup unexpectedly created storage:", err)
			}
			c.HTTP.Session.Stores["sqliteStore"]["connection"] = path
			manager, err = NewSessionManager(context.Background(), c)
			if err != nil {
				t.Fatal("retry failed:", err)
			}
			defer manager.Close()
			store, err := manager.getStore("test")
			if err != nil {
				t.Fatal(err)
			}
			savedSession(t, store)
		})
	}
}

func TestUnselectedSessionProvidersStayInactive(t *testing.T) {
	c := sqliteManagerConfig(filepath.Join(t.TempDir(), "missing", "sessions.db"), "sessions")
	c.HTTP.Session.Types = map[string]string{"default": "cookieStore"}
	m, err := NewSessionManager(context.Background(), c)
	if err != nil {
		t.Fatal("unselected SQLite provider was opened:", err)
	}
	defer m.Close()
	if len(m.stores) != 1 || len(m.closers) != 0 {
		t.Fatal("manager initialized unselected providers")
	}
	m, err = NewSessionManager(context.Background(), &config.Config{})
	if err != nil || len(m.stores) != 0 {
		t.Fatal("empty session configuration requires providers:", err)
	}
	defer m.Close()
}

func TestSQLiteMemoryPoolKeepsSchema(t *testing.T) {
	store, err := NewSQLiteStore(context.Background(), sqliteManagerConfig(":memory:", "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if store.db.Stats().MaxOpenConnections != 1 {
		t.Fatal("in-memory storage must retain one connection")
	}
	var workers sync.WaitGroup
	for range 16 {
		workers.Go(func() {
			var count int
			if err := store.db.Get(&count, "SELECT count(*) FROM sessions"); err != nil {
				t.Error("concurrent request lost the in-memory schema:", err)
			}
		})
	}
	workers.Wait()
}
