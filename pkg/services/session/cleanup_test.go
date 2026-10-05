package session

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSQLiteCleanupBatches(t *testing.T) {
	store := revocationStore(t)
	for i := range 5 {
		expires := time.Now().Add(-time.Hour).In(time.FixedZone("offset", (i-2)*3600))
		if _, err := store.db.Exec(`INSERT INTO sessions (id, session, expires_at) VALUES (?, 'invalid encoded data', ?)`, fmt.Sprint(i), expires); err != nil {
			t.Fatal(err)
		}
	}
	live, request := savedSession(t, store)
	for _, want := range []int64{2, 2, 1, 0} {
		count, err := store.DeleteExpired(context.Background(), 2)
		if err != nil || count != want {
			t.Fatalf("batch deleted %d, %v, want %d", count, err, want)
		}
	}
	loaded, err := store.Get(request, live.Name)
	if err != nil || loaded.ID != live.ID || loaded.Data["identity"] != "account" {
		t.Fatalf("cleanup changed live session: %+v, %v", loaded, err)
	}
	// Setup also adds the index to an existing table without changing its data.
	if _, err := store.db.Exec(`DROP INDEX sessions_expiration`); err != nil {
		t.Fatal(err)
	}
	if err := store.configureDatabase(context.Background()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.db.Get(&count, `SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = 'sessions_expiration'`); err != nil || count != 1 {
		t.Fatalf("expiration index missing: %d, %v", count, err)
	}
}

func TestSQLiteCleanupFailures(t *testing.T) {
	for _, failure := range []string{"zero limit", "negative limit", "canceled", "delete", "missing table", "closed database", "missing database", "missing configuration"} {
		t.Run(failure, func(t *testing.T) {
			store := revocationStore(t)
			if _, err := store.db.Exec(`INSERT INTO sessions (id, expires_at) VALUES ('expired', ?)`, time.Now().Add(-time.Hour)); err != nil {
				t.Fatal(err)
			}
			ctx, limit := context.Background(), 10
			switch failure {
			case "zero limit":
				limit = 0
			case "negative limit":
				limit = -1
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "delete":
				if _, err := store.db.Exec(`CREATE TRIGGER reject_cleanup BEFORE DELETE ON sessions BEGIN SELECT RAISE(ABORT, 'cleanup failed'); END`); err != nil {
					t.Fatal(err)
				}
			case "missing table":
				store.tableName = "absent"
			case "closed database":
				if err := store.db.Close(); err != nil {
					t.Fatal(err)
				}
			case "missing database":
				store.db = nil
			case "missing configuration":
				store.tableName = ""
			}
			count, err := store.DeleteExpired(ctx, limit)
			if err == nil || count != 0 {
				t.Fatalf("failed cleanup = %d, %v", count, err)
			}
			if failure == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation not preserved: %v", err)
			}
			if store.db != nil && failure != "closed database" {
				var remaining int
				if err := store.db.Get(&remaining, `SELECT count(*) FROM sessions`); err != nil || remaining != 1 {
					t.Fatalf("failed cleanup changed storage: %d, %v", remaining, err)
				}
			}
		})
	}
}

func TestSQLiteSaveAndEndAfterCleanup(t *testing.T) {
	store := revocationStore(t)
	original, r := savedSession(t, store)
	if _, err := store.db.Exec(`UPDATE sessions SET expires_at = ? WHERE id = ?`, time.Now().Add(-time.Hour), original.ID); err != nil {
		t.Fatal(err)
	}
	if count, err := store.DeleteExpired(context.Background(), 1); err != nil || count != 1 {
		t.Fatalf("cleanup = %d, %v", count, err)
	}

	w := httptest.NewRecorder()
	var invalid *ErrSessionInvalid
	if err := original.Save(w, r); !errors.As(err, &invalid) || len(w.Result().Cookies()) != 0 {
		t.Fatalf("stale save accepted: %v", err)
	}
	if err := original.End(w, r); err != nil {
		t.Fatal("ending cleaned session:", err)
	}
	loaded, err := store.Get(r, original.Name)
	if err != nil || !loaded.IsNew || len(loaded.Data) != 0 {
		t.Fatalf("cleanup replay = %+v, %v", loaded, err)
	}
}

func TestSQLiteConcurrentSaveAndEnd(t *testing.T) {
	store := revocationStore(t)
	store.db.SetMaxOpenConns(4)
	for range 20 {
		original, r := savedSession(t, store)
		stale, err := store.Get(r, original.Name)
		if err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		saved, ended := make(chan error, 1), make(chan error, 1)
		go func() {
			<-start
			saved <- stale.Save(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
		}()
		go func() {
			<-start
			ended <- original.End(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
		}()
		close(start)
		saveErr, endErr := <-saved, <-ended
		var invalid *ErrSessionInvalid
		if saveErr != nil && !errors.As(saveErr, &invalid) {
			t.Fatal("concurrent save:", saveErr)
		}
		if endErr != nil {
			t.Fatal("concurrent end:", endErr)
		}
		loaded, err := store.Get(r, stale.Name)
		if err != nil || !loaded.IsNew || len(loaded.Data) != 0 {
			t.Fatalf("save restored ended session: %+v, %v", loaded, err)
		}
	}
}

func TestSQLiteCleanupDuringSave(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(fmt.Sprintf("expired=%t", expired), func(t *testing.T) {
			store := revocationStore(t)
			store.db.SetMaxOpenConns(4)
			original, r := savedSession(t, store)
			if expired {
				if _, err := store.db.Exec(`UPDATE sessions SET expires_at = ? WHERE id = ?`, time.Now().Add(-time.Hour), original.ID); err != nil {
					t.Fatal(err)
				}
			}
			start := make(chan struct{})
			saved := make(chan error, 1)
			go func() {
				<-start
				saved <- original.Save(httptest.NewRecorder(), r)
			}()
			close(start)
			count, err := store.DeleteExpired(context.Background(), 1)
			saveErr := <-saved
			if err != nil || (count == 1) != expired {
				t.Fatalf("concurrent cleanup = %d, %v", count, err)
			}
			var invalid *ErrSessionInvalid
			if saveErr != nil && (!expired || !errors.As(saveErr, &invalid)) {
				t.Fatal("concurrent save:", saveErr)
			}
			loaded, err := store.Get(r, original.Name)
			if err != nil || loaded.IsNew != expired {
				t.Fatalf("unexpected session after cleanup: %+v, %v", loaded, err)
			}
		})
	}
}
