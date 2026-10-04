package session

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSQLiteSessionRotation(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "existing"}[existing], func(t *testing.T) {
			store := revocationStore(t)
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			original := newSession(store, "authentication")
			if existing {
				original, r = savedSession(t, store)
			}
			oldID := original.ID
			original.Data["untrusted"] = "discard me"
			alias := original.Data
			unrelated, _ := savedSession(t, store)
			w := httptest.NewRecorder()
			if err := original.Rotate(w, r, map[string]any{"account": "new identity"}); err != nil {
				t.Fatal(err)
			}
			if original.ID == oldID || original.IsNew || len(original.Data) != 1 || original.Data["account"] != "new identity" || len(alias) != 0 {
				t.Fatalf("rotation retained old state: %+v", original)
			}
			if getCachedSession(r, original.Name) != original {
				t.Fatal("rotation did not replace the request cache")
			}
			var count int
			if err := store.db.Get(&count, "SELECT count(*) FROM sessions WHERE id = ?", oldID); err != nil || count != 0 {
				t.Fatalf("old row survived: %d, %v", count, err)
			}
			if err := store.db.Get(&count, "SELECT count(*) FROM sessions WHERE id = ?", unrelated.ID); err != nil || count != 1 {
				t.Fatalf("unrelated session changed: %d, %v", count, err)
			}
			nextRequest := httptest.NewRequest(http.MethodGet, "/", nil)
			for _, cookie := range w.Result().Cookies() {
				nextRequest.AddCookie(cookie)
			}
			loaded, err := store.Get(nextRequest, original.Name)
			if err != nil || loaded.ID != original.ID || loaded.Data["account"] != "new identity" {
				t.Fatalf("replacement cannot be loaded: %+v, %v", loaded, err)
			}
		})
	}
}

func TestSQLiteRotationFailurePreservesSession(t *testing.T) {
	for _, failure := range []string{"delete", "insert", "commit", "canceled", "encoding", "revoked", "expired"} {
		t.Run(failure, func(t *testing.T) {
			store := revocationStore(t)
			original, r := savedSession(t, store)
			oldID := original.ID
			if err := setCachedSession(r, original); err != nil {
				t.Fatal(err)
			}
			data := map[string]any{"account": "new"}
			switch failure {
			case "commit":
				store.db.SetMaxOpenConns(1)
				for _, statement := range []string{
					`PRAGMA foreign_keys = ON`,
					`CREATE TABLE rotation_failure (session_id TEXT REFERENCES sessions(id) DEFERRABLE INITIALLY DEFERRED)`,
					`CREATE TRIGGER reject_rotation AFTER INSERT ON sessions BEGIN INSERT INTO rotation_failure VALUES ('missing'); END`,
				} {
					if _, err := store.db.Exec(statement); err != nil {
						t.Fatal(err)
					}
				}
			case "delete", "insert":
				if _, err := store.db.Exec("CREATE TRIGGER reject_rotation BEFORE " + failure + " ON sessions BEGIN SELECT RAISE(ABORT, 'rotation failed'); END"); err != nil {
					t.Fatal(err)
				}
			case "canceled":
				ctx, cancel := context.WithCancel(r.Context())
				cancel()
				r = r.WithContext(ctx)
			case "encoding":
				data["unsupported"] = make(chan int)
			case "revoked":
				if _, err := store.db.Exec("DELETE FROM sessions WHERE id = ?", oldID); err != nil {
					t.Fatal(err)
				}
			case "expired":
				if _, err := store.db.Exec("UPDATE sessions SET expires_at = ? WHERE id = ?", time.Now().UTC().Add(-time.Hour), oldID); err != nil {
					t.Fatal(err)
				}
			}
			w := httptest.NewRecorder()
			if err := original.Rotate(w, r, data); err == nil {
				t.Fatal("rotation succeeded despite failure")
			}
			if original.ID != oldID || original.Data["identity"] != "account" || getCachedSession(r, original.Name) != original || len(w.Result().Cookies()) != 0 {
				t.Fatal("failed rotation changed local state or cookies")
			}
			var count int
			want := 1
			if failure == "revoked" {
				want = 0
			}
			if err := store.db.Get(&count, "SELECT count(*) FROM sessions"); err != nil || count != want {
				t.Fatalf("failed rotation changed stored rows: %d, %v", count, err)
			}
		})
	}
}

func TestCookieSessionCannotRotate(t *testing.T) {
	s := newSession(&CookieStore{}, "authentication")
	w := httptest.NewRecorder()
	if err := s.Rotate(w, httptest.NewRequest(http.MethodGet, "/", nil), nil); err == nil || len(w.Result().Cookies()) != 0 {
		t.Fatal("cookie-only store accepted revocable rotation")
	}
}

func TestSQLiteConcurrentRotation(t *testing.T) {
	store := revocationStore(t)
	store.db.SetMaxOpenConns(1)
	original, request := savedSession(t, store)
	start := make(chan struct{})
	type result struct {
		session *Session
		writer  *httptest.ResponseRecorder
		err     error
	}
	results := make(chan result, 2)
	for range 2 {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		for _, cookie := range request.Cookies() {
			r.AddCookie(cookie)
		}
		s, err := store.Get(r, original.Name)
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			<-start
			w := httptest.NewRecorder()
			err := s.Rotate(w, r, map[string]any{"account": "identity"})
			results <- result{s, w, err}
		}()
	}
	close(start)
	succeeded := 0
	for range 2 {
		got := <-results
		if got.err == nil {
			succeeded++
			if got.session.ID == original.ID || len(got.writer.Result().Cookies()) != 1 {
				t.Fatal("successful rotation did not publish a replacement")
			}
		} else if got.session.ID != original.ID || len(got.writer.Result().Cookies()) != 0 {
			t.Fatal("failed competing rotation changed session or cookies")
		}
	}
	var count int
	if err := store.db.Get(&count, `SELECT count(*) FROM sessions`); err != nil || succeeded != 1 || count != 1 {
		t.Fatalf("competing rotations: successes=%d, rows=%d, error=%v", succeeded, count, err)
	}
}
