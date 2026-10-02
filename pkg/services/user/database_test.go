package user

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/pkg/database"
)

func TestPrepareDatabaseIndependentDatabases(t *testing.T) {
	first, second := userTestDB(t), userTestDB(t)
	for _, db := range []*sqlx.DB{first, second} {
		u := &User{Email: "person@example.invalid"}
		if err := u.Create(context.Background(), db); err != nil {
			t.Fatal(err)
		}
		token := &AuthToken{UserId: u.ID, TokenHash: "stored-hash", ExpiresAt: time.Now().Add(time.Hour), Type: resetTokenType}
		if err := token.Create(db); err != nil {
			t.Fatal(err)
		}

		if err := PrepareDatabase(context.Background(), db); err != nil {
			t.Fatal("repeated setup:", err)
		}
		stored, err := GetUserByID(db, u.ID)
		if err != nil || stored.Email != u.Email {
			t.Fatalf("account after repeated setup = %v, %v", stored, err)
		}
		var count int
		if err := db.Get(&count, `SELECT count(*) FROM usertoken WHERE user_id = ? AND token_hash = ?`, u.ID, token.TokenHash); err != nil || count != 1 {
			t.Fatalf("token after repeated setup: count = %d, error = %v", count, err)
		}
	}
}

func TestPrepareDatabaseFailures(t *testing.T) {
	t.Run("missing database", func(t *testing.T) {
		var unavailable *database.ErrDatabaseUnavailable
		if err := PrepareDatabase(context.Background(), nil); !errors.As(err, &unavailable) {
			t.Fatalf("missing database error = %v", err)
		}
	})
	t.Run("closed database", func(t *testing.T) {
		db := unpreparedUserTestDB(t)
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		if err := PrepareDatabase(context.Background(), db); err == nil {
			t.Fatal("closed database accepted")
		}
	})
	t.Run("canceled context", func(t *testing.T) {
		db := unpreparedUserTestDB(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := PrepareDatabase(ctx, db); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled setup error = %v", err)
		}
		assertNoAccountTables(t, db)
		if err := PrepareDatabase(context.Background(), db); err != nil {
			t.Fatal("retry after cancellation:", err)
		}
	})
	t.Run("rollback and retry", func(t *testing.T) {
		db := unpreparedUserTestDB(t)
		// An index with the token table's name makes the second CREATE fail.
		if _, err := db.Exec(`CREATE TABLE existing (id TEXT); CREATE INDEX usertoken ON existing(id)`); err != nil {
			t.Fatal(err)
		}
		if err := PrepareDatabase(context.Background(), db); err == nil {
			t.Fatal("conflicting schema accepted")
		}
		assertNoAccountTables(t, db)
		if _, err := db.Exec(`DROP INDEX usertoken`); err != nil {
			t.Fatal(err)
		}
		if err := PrepareDatabase(context.Background(), db); err != nil {
			t.Fatal("retry after failure:", err)
		}
	})
}

func TestAccountOperationsDoNotPrepareDatabase(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(*sqlx.DB) error
	}{
		{"lookup by ID", func(db *sqlx.DB) error {
			_, err := GetUserByID(db, "missing")
			return err
		}},
		{"lookup by email", func(db *sqlx.DB) error {
			_, err := GetUserByEmail(db, "person@example.invalid")
			return err
		}},
		{"create", func(db *sqlx.DB) error {
			return (&User{Email: "person@example.invalid"}).Create(context.Background(), db)
		}},
		{"update", func(db *sqlx.DB) error {
			return (&User{ID: "missing", Email: "person@example.invalid"}).Update(context.Background(), db)
		}},
		{"delete", func(db *sqlx.DB) error { return (&User{ID: "missing"}).Delete(db) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := unpreparedUserTestDB(t)
			if err := tc.run(db); err == nil {
				t.Fatal("account operation succeeded without schema setup")
			}
			assertNoAccountTables(t, db)
		})
	}
}

func assertNoAccountTables(t *testing.T, db *sqlx.DB) {
	t.Helper()
	var count int
	if err := db.Get(&count, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name IN ('user', 'usertoken')`); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("found %d account tables after unsuccessful setup or account access", count)
	}
}
