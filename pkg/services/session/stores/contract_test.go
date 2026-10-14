package stores

import (
	"net/http/httptest"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/trentnix/hyperserver/internal/sessiontest"
	"github.com/trentnix/hyperserver/pkg/services/session"
)

func TestSQLiteSessionContract(t *testing.T) {
	for _, format := range []string{"JSON", "compressed JSON"} {
		t.Run(format, func(t *testing.T) {
			sessiontest.Run(t, func(t *testing.T) session.SessionStore {
				store, err := NewSQLiteStore(t.Context(), sqliteManagerConfig(filepath.Join(t.TempDir(), "sessions.db"), "sessions"))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := store.Close(); err != nil {
						t.Error(err)
					}
				})
				if format == "compressed JSON" {
					store.Serializer = compressedJSONSerializer{}
				}
				return store
			})
		})
	}
}

// SQLite's provider uses Go's clock, so its exact boundary can use synctest.
func TestSQLiteSessionExpiryBoundary(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := revocationStore(t)
		s, r := savedSession(t, store)
		if !s.ExpiresAt.After(time.Now()) {
			t.Fatal("new session must have a future expiry")
		}
		time.Sleep(time.Until(s.ExpiresAt))
		expired, err := store.Get(r, s.Name)
		if err != nil || expired == nil || !expired.IsNew || len(expired.Data) != 0 {
			t.Fatalf("expired session restored identity: %+v, %v", expired, err)
		}
		w := httptest.NewRecorder()
		if next, err := store.Rotate(w, r, s, s.Data); next != nil || err == nil {
			t.Fatalf("expired rotation = %+v, %v", next, err)
		}
		if len(w.Header().Values("Set-Cookie")) != 0 {
			t.Fatal("expired rotation issued a cookie")
		}
	})
}
