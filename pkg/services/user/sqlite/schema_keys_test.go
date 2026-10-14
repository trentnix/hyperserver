package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
)

const accountKeyTestSchema = `CREATE TABLE user (
	id VARCHAR(36) PRIMARY KEY,
	email VARCHAR(255) NOT NULL,
	email_key TEXT NOT NULL UNIQUE,
	verified BOOLEAN NOT NULL DEFAULT FALSE,
	verification_required BOOLEAN NOT NULL DEFAULT FALSE,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	registration_auth_type VARCHAR(50),
	password VARCHAR(255),
	session_version INTEGER NOT NULL DEFAULT 1
)`

const tokenKeyTestSchema = `CREATE TABLE usertoken (
	user_id VARCHAR(36),
	token_hash VARCHAR(255) NOT NULL,
	expires_at TIMESTAMP NOT NULL,
	token_type VARCHAR(255) NOT NULL,
	PRIMARY KEY (user_id, token_hash),
	FOREIGN KEY (user_id) REFERENCES user(id) ON DELETE CASCADE
)`

func TestAccountSchemaUniqueKeys(t *testing.T) {
	for _, tc := range []struct {
		name        string
		old, change string
		index       string
		wantError   string
	}{
		{name: "current schema"},
		{name: "missing account ID key", old: "id VARCHAR(36) PRIMARY KEY", change: "id VARCHAR(36)", wantError: "user requires"},
		{name: "nonbinary account ID key", old: "id VARCHAR(36) PRIMARY KEY", change: "id VARCHAR(36)", index: "CREATE UNIQUE INDEX account_id_index ON user(id COLLATE NOCASE)", wantError: "user requires"},
		{name: "missing email key", old: "email_key TEXT NOT NULL UNIQUE", change: "email_key TEXT NOT NULL", wantError: "email_key"},
		{name: "missing token key", old: "PRIMARY KEY (user_id, token_hash),", wantError: "usertoken requires"},
		{name: "ordinary email index", old: "email_key TEXT NOT NULL UNIQUE", change: "email_key TEXT NOT NULL", index: "CREATE INDEX email_index ON user(email_key)", wantError: "email_key"},
		{name: "partial email index", old: "email_key TEXT NOT NULL UNIQUE", change: "email_key TEXT NOT NULL", index: "CREATE UNIQUE INDEX email_index ON user(email_key) WHERE verified = 1", wantError: "email_key"},
		{name: "composite email index", old: "email_key TEXT NOT NULL UNIQUE", change: "email_key TEXT NOT NULL", index: "CREATE UNIQUE INDEX email_index ON user(email_key, id)", wantError: "email_key"},
		{name: "expression email index", old: "email_key TEXT NOT NULL UNIQUE", change: "email_key TEXT NOT NULL", index: "CREATE UNIQUE INDEX email_index ON user(lower(email_key))", wantError: "email_key"},
		{name: "column and expression index", old: "email_key TEXT NOT NULL UNIQUE", change: "email_key TEXT NOT NULL", index: "CREATE UNIQUE INDEX email_index ON user(email_key, lower(email))", wantError: "email_key"},
		{name: "nonbinary email index", old: "email_key TEXT NOT NULL UNIQUE", change: "email_key TEXT NOT NULL", index: "CREATE UNIQUE INDEX email_index ON user(email_key COLLATE NOCASE)", wantError: "email_key"},
		{name: "partial token key", old: "PRIMARY KEY (user_id, token_hash),", index: "CREATE UNIQUE INDEX token_index ON usertoken(user_id, token_hash) WHERE token_type = 'auth-reset'", wantError: "usertoken requires"},
		{name: "extra token key column", old: "PRIMARY KEY (user_id, token_hash),", index: "CREATE UNIQUE INDEX token_index ON usertoken(user_id, token_hash, token_type)", wantError: "usertoken requires"},
		{name: "mixed token collations", old: "PRIMARY KEY (user_id, token_hash),", index: "CREATE UNIQUE INDEX token_index ON usertoken(user_id, token_hash COLLATE NOCASE)", wantError: "usertoken requires"},
		{name: "explicit account ID index", old: "id VARCHAR(36) PRIMARY KEY", change: "id VARCHAR(36)", index: "CREATE UNIQUE INDEX account_id_index ON user(id)"},
		{name: "named email index", old: "email_key TEXT NOT NULL UNIQUE", change: "email_key TEXT NOT NULL", index: `CREATE UNIQUE INDEX "email "" key" ON user(email_key DESC)`},
		{name: "reordered token key", old: "PRIMARY KEY (user_id, token_hash)", change: "PRIMARY KEY (token_hash, user_id)"},
		{name: "explicit token key", old: "PRIMARY KEY (user_id, token_hash),", index: "CREATE UNIQUE INDEX token_index ON usertoken(token_hash DESC, user_id)"},
		{name: "irrelevant extra index", index: "CREATE UNIQUE INDEX email_expression ON user(lower(email))"},
		{name: "uppercase column name", old: "email_key", change: "EMAIL_KEY"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			schema := accountKeyTestSchema + ";" + tokenKeyTestSchema
			if tc.old != "" {
				if !strings.Contains(schema, tc.old) {
					t.Fatal("fixture does not contain the schema being changed")
				}
				schema = strings.Replace(schema, tc.old, tc.change, 1)
			}
			db := unpreparedUserTestDB(t)
			if _, err := db.ExecContext(t.Context(), schema); err != nil {
				t.Fatal(err)
			}
			if tc.index != "" {
				if _, err := db.ExecContext(t.Context(), tc.index); err != nil {
					t.Fatal(err)
				}
			}

			for range 2 {
				err := NewAccountRepository(db).Initialize(t.Context())
				if tc.wantError == "" {
					if err != nil {
						t.Fatal("compatible schema rejected:", err)
					}
				} else if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("incompatible key accepted or poorly identified: %v", err)
				}
			}
		})
	}
}

func TestAccountKeysWithoutRowID(t *testing.T) {
	db := unpreparedUserTestDB(t)
	if _, err := db.ExecContext(t.Context(), accountKeyTestSchema+" WITHOUT ROWID;"+tokenKeyTestSchema+" WITHOUT ROWID"); err != nil {
		t.Fatal(err)
	}
	if err := NewAccountRepository(db).Initialize(t.Context()); err != nil {
		t.Fatal("compatible WITHOUT ROWID schema rejected:", err)
	}
}

func TestMissingAccountKeyPreservesDataAndRollsBack(t *testing.T) {
	db := unpreparedUserTestDB(t)
	schema := strings.Replace(accountKeyTestSchema, "email_key TEXT NOT NULL UNIQUE", "email_key TEXT NOT NULL", 1)
	if _, err := db.ExecContext(t.Context(), schema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `INSERT INTO user (id, email, email_key, password) VALUES
		('first', 'duplicate@example.invalid', 'duplicate', 'first hash'),
		('second', 'duplicate@example.invalid', 'duplicate', 'second hash')`); err != nil {
		t.Fatal(err)
	}

	if err := NewAccountRepository(db).Initialize(t.Context()); err == nil || !strings.Contains(err.Error(), "email_key") {
		t.Fatalf("missing uniqueness accepted: %v", err)
	}
	var count int
	if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM user WHERE email_key = 'duplicate'").Scan(&count); err != nil || count != 2 {
		t.Fatalf("startup changed duplicate records: %d, %v", count, err)
	}
	if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM sqlite_master WHERE name IN ('usertoken', 'usertoken_expiration')").Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed setup left bootstrap objects: %d, %v", count, err)
	}

	// Repair belongs to an explicit maintenance operation, not provider startup.
	if _, err := db.ExecContext(t.Context(), `UPDATE user SET email_key = 'distinct' WHERE id = 'second';
		CREATE UNIQUE INDEX repaired_email_key ON user(email_key)`); err != nil {
		t.Fatal(err)
	}
	if err := NewAccountRepository(db).Initialize(t.Context()); err != nil {
		t.Fatal("retry after explicit repair:", err)
	}
	for _, id := range []string{"first", "second"} {
		var password string
		if err := db.QueryRowContext(t.Context(), "SELECT password FROM user WHERE id = ?", id).Scan(&password); err != nil || password != id+" hash" {
			t.Fatalf("startup changed credentials: %q, %v", password, err)
		}
	}
}

func TestAccountKeyInspectionFailures(t *testing.T) {
	for _, failure := range []string{"canceled", "finished transaction"} {
		t.Run(failure, func(t *testing.T) {
			db := userTestDB(t)
			tx, err := db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			want := context.Canceled
			if failure == "canceled" {
				cancel()
			} else {
				want = sql.ErrTxDone
				if err := tx.Rollback(); err != nil {
					t.Fatal(err)
				}
			}
			if err := requireAccountUniqueKey(ctx, tx, "user", []string{"email_key"}); !errors.Is(err, want) {
				t.Fatalf("inspection lost its underlying failure: %v", err)
			}
		})
	}
}
