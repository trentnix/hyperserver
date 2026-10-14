package sqlite

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/trentnix/hyperserver/internal/accounttest"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestSQLiteAccountContract(t *testing.T) {
	accounttest.Run(t, func(t *testing.T) user.AccountRepository {
		db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "accounts.db")+"?_journal=WAL&_timeout=5000&_fk=true")
		if err != nil {
			t.Fatal(err)
		}
		// Use two connections so competing writes also exercise SQLite's locking.
		db.SetMaxOpenConns(2)
		t.Cleanup(func() {
			if err := db.Close(); err != nil {
				t.Error(err)
			}
		})
		repo := NewAccountRepository(db)
		if err := repo.Initialize(t.Context()); err != nil {
			t.Fatal(err)
		}
		return repo
	})
}
