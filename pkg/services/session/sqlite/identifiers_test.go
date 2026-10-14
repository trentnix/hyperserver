package sqlite

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestConfiguredTableNameSessionLifecycle(t *testing.T) {
	for _, table := range []string{"session-data", "session data", "select", `session"data`, `sessions"; DROP TABLE sentinel; --`} {
		t.Run(table, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sessions.db")
			store, err := NewSQLiteStore(t.Context(), sqliteStoreOptions(path, table))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { store.Close() })
			if _, err := store.db.ExecContext(t.Context(), "CREATE TABLE sentinel (id INTEGER)"); err != nil {
				t.Fatal(err)
			}
			if err := store.configureDatabase(t.Context()); err != nil {
				t.Fatal("repeated initialization:", err)
			}
			var indexCount int
			if err := store.db.QueryRowContext(t.Context(), "SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = ?", table+"_expiration").Scan(&indexCount); err != nil || indexCount != 1 {
				t.Fatalf("expiration index = %d, %v", indexCount, err)
			}

			original, request := savedSession(t, store)
			loaded, err := store.Get(request, original.Name)
			if err != nil || loaded.ID != original.ID || loaded.Data["identity"] != "account" {
				t.Fatalf("read = %+v, %v", loaded, err)
			}
			loaded.Data["identity"] = "updated"
			if err := loaded.Save(httptest.NewRecorder(), request); err != nil {
				t.Fatal("update:", err)
			}
			freshRequest := httptest.NewRequest(http.MethodGet, "/", nil)
			for _, cookie := range request.Cookies() {
				freshRequest.AddCookie(cookie)
			}
			loaded, err = store.Get(freshRequest, original.Name)
			if err != nil || loaded.Data["identity"] != "updated" {
				t.Fatalf("updated read = %+v, %v", loaded, err)
			}

			recorder := httptest.NewRecorder()
			if err := loaded.Rotate(recorder, freshRequest, map[string]any{"identity": "rotated"}); err != nil {
				t.Fatal("rotate:", err)
			}
			var oldCount int
			if err := store.db.QueryRowContext(t.Context(), "SELECT count(*) FROM "+quoteIdentifier(table)+" WHERE id = ?", original.ID).Scan(&oldCount); err != nil || oldCount != 0 {
				t.Fatalf("old session survived rotation: %d, %v", oldCount, err)
			}
			rotatedRequest := httptest.NewRequest(http.MethodGet, "/", nil)
			for _, cookie := range recorder.Result().Cookies() {
				rotatedRequest.AddCookie(cookie)
			}
			rotated, err := store.Get(rotatedRequest, original.Name)
			if err != nil || rotated.Data["identity"] != "rotated" {
				t.Fatalf("rotated read = %+v, %v", rotated, err)
			}
			if err := rotated.End(httptest.NewRecorder(), rotatedRequest); err != nil {
				t.Fatal("end:", err)
			}

			expired, _ := savedSession(t, store)
			if _, err := store.db.ExecContext(t.Context(), "UPDATE "+quoteIdentifier(table)+" SET expires_at = ? WHERE id = ?", time.Now().Add(-time.Hour), expired.ID); err != nil {
				t.Fatal(err)
			}
			maintenance, closeStore, err := OpenMaintenance(t.Context(), path, table)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { closeStore() })
			if count, err := maintenance.DeleteExpired(t.Context(), 1); err != nil || count != 1 {
				t.Fatalf("cleanup = %d, %v", count, err)
			}
			var remaining int
			if err := store.db.QueryRowContext(t.Context(), "SELECT count(*) FROM "+quoteIdentifier(table)).Scan(&remaining); err != nil || remaining != 0 {
				t.Fatalf("sessions remained: %d, %v", remaining, err)
			}
			if err := store.db.QueryRowContext(t.Context(), "SELECT count(*) FROM sentinel").Scan(&remaining); err != nil {
				t.Fatal("configured identifier changed unrelated storage:", err)
			}
		})
	}
}
