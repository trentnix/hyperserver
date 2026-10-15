package sqlite

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/pkg/services/session/cookie"
)

func sqliteStoreOptions(path, table string) Options {
	return Options{
		Connection:     path,
		Table:          table,
		SigningKey:     strings.Repeat("s", 32),
		TokenLifetime:  time.Hour,
		CookieLifetime: time.Hour,
	}
}

func TestSQLitePoolPolicy(t *testing.T) {
	for _, maintenance := range []bool{false, true} {
		for _, disableForeignKeys := range []bool{false, true} {
			t.Run(fmt.Sprintf("maintenance=%t/disableForeignKeys=%t", maintenance, disableForeignKeys), func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "sessions.db")
				if maintenance {
					if err := os.WriteFile(path, nil, 0600); err != nil {
						t.Fatal(err)
					}
				}
				connection := path + "?_timeout=75"
				if disableForeignKeys {
					connection += "&_fk=false"
				}
				var store *SQLiteStore
				var err error
				if maintenance {
					opened, closeStore, openErr := OpenMaintenance(t.Context(), connection, "sessions")
					err = openErr
					if closeStore != nil {
						t.Cleanup(func() { _ = closeStore() })
					}
					if opened != nil {
						store = opened.(*SQLiteStore)
					}
				} else {
					store, err = NewSQLiteStore(t.Context(), sqliteStoreOptions(connection, "sessions"))
				}
				if disableForeignKeys {
					if store != nil {
						store.Close()
						t.Fatal("invalid options returned a store")
					}
					if err == nil {
						t.Fatal("disabled foreign keys accepted")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = store.Close() })
				if store.db.Stats().MaxOpenConnections != 1 {
					t.Fatal("session pool must limit open connections")
				}
				for pragma, want := range map[string]int{"foreign_keys": 1, "busy_timeout": 75} {
					var got int
					if err := store.db.QueryRowContext(t.Context(), "PRAGMA "+pragma).Scan(&got); err != nil || got != want {
						t.Fatalf("%s = %d, %v, want %d", pragma, got, err, want)
					}
				}
			})
		}
	}
}

func TestConcurrentColdSQLiteInitialization(t *testing.T) {
	c := sqliteStoreOptions(filepath.Join(t.TempDir(), "sessions.db"), "sessions")
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

func TestSQLiteMemoryPoolKeepsSchema(t *testing.T) {
	store, err := NewSQLiteStore(context.Background(), sqliteStoreOptions(":memory:", "sessions"))
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
			if err := store.db.QueryRowContext(t.Context(), "SELECT count(*) FROM sessions").Scan(&count); err != nil {
				t.Error("concurrent request lost the in-memory schema:", err)
			}
		})
	}
	workers.Wait()
}

func setupCookieStore(t *testing.T) *cookie.CookieStore {
	t.Helper()
	store, err := cookie.NewCookieStore(cookie.Options{
		SigningKey:     "secretKey00000000000000000000000",
		TokenLifetime:  time.Hour,
		CookieLifetime: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	return store
}
