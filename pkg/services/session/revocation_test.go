package session

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/pkg/database"
)

func revocationStore(t *testing.T) *SQLiteStore {
	t.Helper()
	db, err := database.Setup("sqlite3", filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := &SQLiteStore{db: db, tableName: "sessions", JwtKey: []byte("revocation-test-key"), TokenLifetime: time.Hour, CookieLifetime: time.Hour, enabled: true}
	if err := s.configureDatabase(); err != nil {
		t.Fatal(err)
	}
	return s
}

func savedSession(t *testing.T, store SessionStore) (*Session, *http.Request) {
	t.Helper()
	s := newSession(store, "test-session")
	s.Data["identity"] = "account"
	w := httptest.NewRecorder()
	if err := s.Save(w, httptest.NewRequest(http.MethodGet, "/", nil)); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, cookie := range w.Result().Cookies() {
		r.AddCookie(cookie)
	}
	return s, r
}

func TestSQLiteSessionRevocation(t *testing.T) {
	store := revocationStore(t)
	original, r := savedSession(t, store)
	id := original.ID
	other, _ := savedSession(t, store)
	stale, err := store.Get(r, original.Name)
	if err != nil {
		t.Fatal(err)
	}
	if err := setCachedSession(r, original); err != nil {
		t.Fatal(err)
	}
	oldData := original.Data
	w := httptest.NewRecorder()
	if err := original.End(w, r); err != nil {
		t.Fatal(err)
	}
	if cookies := w.Result().Cookies(); len(cookies) != 1 || cookies[0].MaxAge != -1 {
		t.Fatalf("logout cookies = %v", cookies)
	}
	if len(oldData) != 0 || len(original.Data) != 0 || original.ID == id || !original.IsNew {
		t.Fatal("logout retained local session identity")
	}
	if cached, err := Get(r, original.Name); err != nil || cached != original {
		t.Fatalf("logout cache = %v, %v", cached, err)
	}
	loaded, err := store.Get(r, original.Name)
	if err != nil || !loaded.IsNew || len(loaded.Data) != 0 || loaded.ID == id {
		t.Fatalf("captured cookie restored a revoked session: %+v, %v", loaded, err)
	}
	var count int
	if err := store.db.Get(&count, `SELECT count(*) FROM sessions WHERE id = ?`, other.ID); err != nil || count != 1 {
		t.Fatal("logout removed an unrelated session", err)
	}

	w = httptest.NewRecorder()
	var invalid *ErrSessionInvalid
	if err := stale.Save(w, r); !errors.As(err, &invalid) || len(w.Result().Cookies()) != 0 {
		t.Fatalf("stale save did not reject revocation: %v", err)
	}
	// Ending the already deleted ID is safe and still expires the browser cookie.
	if err := store.End(httptest.NewRecorder(), r, stale); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteStoredExpiration(t *testing.T) {
	for _, state := range []string{"active", "expired", "zero"} {
		t.Run(state, func(t *testing.T) {
			store := revocationStore(t)
			original, r := savedSession(t, store)
			expires := time.Now().Add(time.Hour)
			if state == "expired" {
				expires = time.Now().Add(-time.Minute)
			}
			if state == "zero" {
				expires = time.Time{}
			}
			if _, err := store.db.Exec(`UPDATE sessions SET expires_at = ? WHERE id = ?`, expires, original.ID); err != nil {
				t.Fatal(err)
			}
			if state != "active" {
				// Expired data must not be decoded or restored, even with a valid signed cookie.
				if _, err := store.db.Exec(`UPDATE sessions SET session = 'invalid base64!' WHERE id = ?`, original.ID); err != nil {
					t.Fatal(err)
				}
			}
			loaded, err := store.Get(r, original.Name)
			if err != nil {
				t.Fatal(err)
			}
			if state == "active" {
				if loaded.IsNew || loaded.ID != original.ID || loaded.Data["identity"] != "account" {
					t.Fatal("active session was not restored")
				}
			} else if !loaded.IsNew || loaded.ID == original.ID || len(loaded.Data) != 0 {
				t.Fatal("expired stored session was restored")
			}
		})
	}
}

func TestSQLiteSaveDoesNotExtendStoredExpiration(t *testing.T) {
	store := revocationStore(t)
	original, r := savedSession(t, store)
	expired := time.Now().Add(-time.Minute)
	if _, err := store.db.Exec(`UPDATE sessions SET expires_at = ? WHERE id = ?`, expired, original.ID); err != nil {
		t.Fatal(err)
	}
	if err := original.Save(httptest.NewRecorder(), r); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Get(r, original.Name)
	if err != nil || !loaded.IsNew || len(loaded.Data) != 0 {
		t.Fatalf("save revived expired storage: %+v, %v", loaded, err)
	}
}

func TestSQLiteEndFailurePreservesState(t *testing.T) {
	for _, failure := range []string{"delete failure", "cancellation", "missing pool", "missing table", "empty id"} {
		t.Run(failure, func(t *testing.T) {
			store := revocationStore(t)
			s, r := savedSession(t, store)
			id := s.ID
			if err := setCachedSession(r, s); err != nil {
				t.Fatal(err)
			}
			switch failure {
			case "delete failure":
				if _, err := store.db.Exec(`CREATE TRIGGER reject_delete BEFORE DELETE ON sessions BEGIN SELECT RAISE(ABORT, 'delete failed'); END`); err != nil {
					t.Fatal(err)
				}
			case "cancellation":
				ctx, cancel := context.WithCancel(r.Context())
				cancel()
				r = r.WithContext(ctx)
			case "missing pool":
				store.db = nil
			case "missing table":
				store.tableName = ""
			case "empty id":
				s.ID = ""
			}
			w := httptest.NewRecorder()
			err := s.End(w, r)
			if err == nil {
				t.Fatal("failed logout reported success")
			}
			if failure == "cancellation" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation error = %v", err)
			}
			if s.Data["identity"] != "account" || getCachedSession(r, s.Name) != s || len(w.Result().Cookies()) != 0 {
				t.Fatal("failed logout changed cookies or local identity")
			}
			if store.db != nil {
				var count int
				if err := store.db.Get(&count, `SELECT count(*) FROM sessions WHERE id = ?`, id); err != nil || count != 1 {
					t.Fatal("failed logout removed the stored session", err)
				}
			}
		})
	}
}

func TestSQLiteSessionReadCancellation(t *testing.T) {
	store := revocationStore(t)
	s, r := savedSession(t, store)
	ctx, cancel := context.WithCancel(r.Context())
	cancel()
	if loaded, err := store.Get(r.WithContext(ctx), s.Name); loaded != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read = %v, %v", loaded, err)
	}
}

func TestCookieSessionEndClearsRequestCache(t *testing.T) {
	store := setupCookieStore(t)
	s, r := savedSession(t, store)
	id := s.ID
	if err := setCachedSession(r, s); err != nil {
		t.Fatal(err)
	}
	if err := s.End(httptest.NewRecorder(), r); err != nil {
		t.Fatal(err)
	}
	loaded, err := Get(r, s.Name)
	if err != nil || !loaded.IsNew || loaded.ID == id || len(loaded.Data) != 0 {
		t.Fatalf("ended cookie session remained cached: %+v, %v", loaded, err)
	}
	// Cookie-only storage cannot revoke the signed cookie held by another request.
	if replay, err := store.Get(r, s.Name); err != nil || replay.ID != id {
		t.Fatalf("unexpected cookie-only replay behavior: %+v, %v", replay, err)
	}
}
