package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/pkg/database"
	accounts "github.com/trentnix/hyperserver/pkg/services/user"
)

func TestPrepareDatabaseIndependentDatabases(t *testing.T) {
	first, second := userTestDB(t), userTestDB(t)
	for _, db := range []*sql.DB{first, second} {
		u := &accounts.User{Email: "person@example.invalid"}
		if err := NewAccountRepository(db).Create(context.Background(), u); err != nil {
			t.Fatal(err)
		}
		token := &accounts.AuthToken{UserId: u.ID, TokenHash: "stored-hash", ExpiresAt: time.Now().Add(time.Hour), Type: accounts.ResetTokenPurpose}
		if err := NewAccountRepository(db).InsertToken(t.Context(), token.Metadata()); err != nil {
			t.Fatal(err)
		}

		if err := PrepareDatabase(context.Background(), db); err != nil {
			t.Fatal("repeated setup:", err)
		}
		stored, err := NewAccountRepository(db).GetByID(t.Context(), u.ID)
		if err != nil || stored.Email != u.Email {
			t.Fatalf("account after repeated setup = %v, %v", stored, err)
		}
		var count int
		if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM usertoken WHERE user_id = ? AND token_hash = ?`, u.ID, token.TokenHash).Scan(&count); err != nil || count != 1 {
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
	t.Run("name conflict and retry", func(t *testing.T) {
		db := unpreparedUserTestDB(t)
		// A reserved name already in use must prevent fresh setup.
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
		run  func(*sql.DB) error
	}{
		{"lookup by ID", func(db *sql.DB) error {
			_, err := NewAccountRepository(db).GetByID(t.Context(), "missing")
			return err
		}},
		{"lookup by email", func(db *sql.DB) error {
			_, err := NewAccountRepository(db).GetByEmail(t.Context(), "person@example.invalid")
			return err
		}},
		{"create", func(db *sql.DB) error {
			return NewAccountRepository(db).Create(context.Background(), &accounts.User{Email: "person@example.invalid"})
		}},
		{"update", func(db *sql.DB) error {
			return NewAccountRepository(db).Update(context.Background(), &accounts.User{ID: "missing", Email: "person@example.invalid"})
		}},
		{"create token", func(db *sql.DB) error {
			return NewAccountRepository(db).InsertToken(t.Context(), accounts.TokenMetadata{
				AccountID: "missing", TokenHash: "hash", Purpose: accounts.ResetTokenPurpose, ExpiresAt: time.Now().Add(time.Hour),
			})
		}},
		{"lookup token", func(db *sql.DB) error {
			_, err := NewAccountRepository(db).GetToken(t.Context(), "hash", accounts.ResetTokenPurpose)
			return err
		}},
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

func assertNoAccountTables(t *testing.T, db *sql.DB) {
	t.Helper()
	var count int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name IN ('user', 'usertoken', 'hyperserver_account_version')`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("found %d account tables after unsuccessful setup or account access", count)
	}
}

func TestInitializeRejectsMissingAccountColumns(t *testing.T) {
	for _, schema := range []struct {
		table   string
		columns []string
	}{
		{"user", []string{
			"id", "email", "email_key", "verified", "verification_required",
			"created_at", "updated_at", "registration_auth_type", "password", "session_version",
		}},
		{"usertoken", []string{"user_id", "token_hash", "expires_at", "token_type"}},
	} {
		for _, column := range schema.columns {
			t.Run(schema.table+"/"+column, func(t *testing.T) {
				db := userTestDB(t)
				repository := NewAccountRepository(db)
				account := &accounts.User{Email: "stored@example.invalid", Password: "preserved", RegistrationAuthType: "email"}
				if err := repository.Create(t.Context(), account); err != nil {
					t.Fatal(err)
				}
				token := accounts.TokenMetadata{
					AccountID: account.ID, TokenHash: "preserved-hash",
					Purpose: accounts.ResetTokenPurpose, ExpiresAt: time.Now().Add(time.Hour).UTC(),
				}
				if err := repository.InsertToken(t.Context(), token); err != nil {
					t.Fatal(err)
				}
				before, err := repository.GetByID(t.Context(), account.ID)
				if err != nil {
					t.Fatal(err)
				}

				// These identifiers come only from the fixed test cases above.
				if _, err := db.ExecContext(t.Context(), "ALTER TABLE "+schema.table+" RENAME COLUMN "+column+" TO legacy_column"); err != nil {
					t.Fatal(err)
				}
				for range 2 {
					err := repository.Initialize(t.Context())
					if err == nil || !strings.Contains(err.Error(), "check SQLite account schema columns") || !strings.Contains(err.Error(), column) {
						t.Fatalf("missing column accepted or poorly identified: %v", err)
					}
					if strings.Contains(err.Error(), account.Email) || strings.Contains(err.Error(), token.TokenHash) {
						t.Fatal("schema error exposed stored data")
					}
				}

				// An explicit repair allows retry without replacing stored records.
				if _, err := db.ExecContext(t.Context(), "ALTER TABLE "+schema.table+" RENAME COLUMN legacy_column TO "+column); err != nil {
					t.Fatal(err)
				}
				if err := repository.Initialize(t.Context()); err != nil {
					t.Fatal("retry after repair:", err)
				}
				after, err := repository.GetByID(t.Context(), account.ID)
				if err != nil || *after != *before {
					t.Fatalf("startup changed the account: %+v, %v", after, err)
				}
				storedToken, err := repository.GetToken(t.Context(), token.TokenHash, token.Purpose)
				if err != nil || storedToken.AccountID != token.AccountID || storedToken.TokenHash != token.TokenHash || storedToken.Purpose != token.Purpose || !storedToken.ExpiresAt.Equal(token.ExpiresAt) {
					t.Fatalf("startup changed the token: %+v, %v", storedToken, err)
				}
			})
		}
	}
}

func TestIncompatibleAccountSchemaDoesNotCreateMissingTables(t *testing.T) {
	db := versionedAccountTestDB(t)
	if _, err := db.ExecContext(t.Context(), "CREATE TABLE usertoken (legacy_value TEXT); INSERT INTO usertoken VALUES ('preserved')"); err != nil {
		t.Fatal(err)
	}
	if err := NewAccountRepository(db).Initialize(t.Context()); err == nil || !strings.Contains(err.Error(), "check SQLite account schema columns") {
		t.Fatalf("incompatible table accepted: %v", err)
	}
	var count int
	if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM sqlite_master WHERE name IN ('user', 'usertoken_expiration')").Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed startup left bootstrap objects: %d, %v", count, err)
	}
	var value string
	if err := db.QueryRowContext(t.Context(), "SELECT legacy_value FROM usertoken").Scan(&value); err != nil || value != "preserved" {
		t.Fatalf("failed startup changed existing data: %q, %v", value, err)
	}
}
