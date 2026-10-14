package stores

import (
	"path/filepath"
	"testing"

	"github.com/trentnix/hyperserver/internal/sessiontest"
	"github.com/trentnix/hyperserver/pkg/services/session"
)

func TestSQLiteSessionContract(t *testing.T) {
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
		return store
	})
}
