package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
)

func TestTokenForeignKeyDeclarations(t *testing.T) {
	const required = "FOREIGN KEY (user_id) REFERENCES user(id) ON DELETE CASCADE"
	accountWithUniqueID := strings.Replace(accountKeyTestSchema, "id VARCHAR(36) PRIMARY KEY", "id VARCHAR(36) UNIQUE", 1)

	for _, tc := range []struct {
		name, declaration string
		accountSchema     string
		wantError         bool
	}{
		{name: "explicit parent", declaration: required},
		{name: "implicit primary key", declaration: "FOREIGN KEY (user_id) REFERENCES user ON DELETE CASCADE"},
		{name: "named constraint", declaration: "CONSTRAINT account_reference " + required},
		{name: "identifier case", declaration: "FOREIGN KEY (USER_ID) REFERENCES USER(ID) ON DELETE CASCADE"},
		{name: "deferred", declaration: required + " DEFERRABLE INITIALLY DEFERRED"},
		{name: "update cascade", declaration: required + " ON UPDATE CASCADE"},
		{name: "duplicate relationship", declaration: required + ", " + required},
		{name: "explicit unique parent", declaration: required, accountSchema: accountWithUniqueID},
		{name: "missing", wantError: true},
		{name: "no delete action", declaration: "FOREIGN KEY (user_id) REFERENCES user(id)", wantError: true},
		{name: "restrict delete", declaration: "FOREIGN KEY (user_id) REFERENCES user(id) ON DELETE RESTRICT", wantError: true},
		{name: "set null", declaration: "FOREIGN KEY (user_id) REFERENCES user(id) ON DELETE SET NULL", wantError: true},
		{name: "set default", declaration: "FOREIGN KEY (user_id) REFERENCES user(id) ON DELETE SET DEFAULT", wantError: true},
		{name: "wrong table", declaration: "FOREIGN KEY (user_id) REFERENCES another_user(id) ON DELETE CASCADE", wantError: true},
		{name: "wrong child", declaration: "FOREIGN KEY (token_hash) REFERENCES user(id) ON DELETE CASCADE", wantError: true},
		{name: "wrong parent", declaration: "FOREIGN KEY (user_id) REFERENCES user(email_key) ON DELETE CASCADE", wantError: true},
		{name: "separate incomplete relationships", declaration: "FOREIGN KEY (user_id) REFERENCES user(id), FOREIGN KEY (token_hash) REFERENCES user(id) ON DELETE CASCADE", wantError: true},
		{name: "composite", declaration: "FOREIGN KEY (user_id, token_hash) REFERENCES user(id, email_key) ON DELETE CASCADE", wantError: true},
		{
			name: "implicit missing primary key", declaration: "FOREIGN KEY (user_id) REFERENCES user ON DELETE CASCADE",
			accountSchema: accountWithUniqueID, wantError: true,
		},
		{
			name: "implicit wrong primary key", declaration: "FOREIGN KEY (user_id) REFERENCES user ON DELETE CASCADE",
			accountSchema: strings.Replace(accountWithUniqueID, "email_key TEXT NOT NULL UNIQUE", "email_key TEXT NOT NULL PRIMARY KEY", 1), wantError: true,
		},
		{
			name: "implicit composite primary key", declaration: "FOREIGN KEY (user_id) REFERENCES user ON DELETE CASCADE",
			accountSchema: strings.Replace(accountWithUniqueID, "session_version INTEGER NOT NULL DEFAULT 1", "session_version INTEGER NOT NULL DEFAULT 1, PRIMARY KEY (id, email_key)", 1), wantError: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := unpreparedUserTestDB(t)
			accountSchema := tc.accountSchema
			if accountSchema == "" {
				accountSchema = accountKeyTestSchema
			}
			tokenSchema := strings.Replace(tokenKeyTestSchema, required, tc.declaration, 1)
			if tc.declaration == "" {
				tokenSchema = strings.Replace(tokenKeyTestSchema, ",\n\t"+required, "", 1)
			}
			if _, err := db.ExecContext(t.Context(), "PRAGMA foreign_keys = ON;"+accountSchema+";"+tokenSchema); err != nil {
				t.Fatal(err)
			}

			for range 2 {
				err := NewAccountRepository(db).Initialize(t.Context())
				if tc.wantError {
					if err == nil || !strings.Contains(err.Error(), "usertoken.user_id requires a foreign key to user(id) with ON DELETE CASCADE") {
						t.Fatalf("incompatible foreign key accepted or poorly identified: %v", err)
					}
				} else if err != nil {
					t.Fatal("compatible foreign key rejected:", err)
				}
			}
			if tc.wantError {
				return
			}

			// Exercise enforcement and cascading deletion for every accepted shape.
			if _, err := db.ExecContext(t.Context(), `INSERT INTO usertoken VALUES ('missing', 'orphan', CURRENT_TIMESTAMP, 'auth-reset')`); err == nil {
				t.Fatal("token without an account accepted")
			}
			if _, err := db.ExecContext(t.Context(), `INSERT INTO user (id, email, email_key) VALUES ('account', 'person@example.invalid', 'person@example.invalid');
				INSERT INTO usertoken VALUES ('account', 'stored', CURRENT_TIMESTAMP, 'auth-reset');
				DELETE FROM user WHERE id = 'account'`); err != nil {
				t.Fatal(err)
			}
			var count int
			if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM usertoken").Scan(&count); err != nil || count != 0 {
				t.Fatalf("account deletion left tokens: %d, %v", count, err)
			}
		})
	}
}

func TestTokenForeignKeyFailurePreservesDataAndAllowsRepair(t *testing.T) {
	db := unpreparedUserTestDB(t)
	schema := strings.Replace(tokenKeyTestSchema, ",\n\tFOREIGN KEY (user_id) REFERENCES user(id) ON DELETE CASCADE", "", 1)
	if _, err := db.ExecContext(t.Context(), schema+`; INSERT INTO usertoken VALUES ('account', 'preserved', '2030-01-01', 'auth-reset')`); err != nil {
		t.Fatal(err)
	}
	if err := NewAccountRepository(db).Initialize(t.Context()); err == nil {
		t.Fatal("missing foreign key accepted")
	}
	var count int
	if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM sqlite_master WHERE name IN ('user', 'usertoken_expiration')").Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed setup left bootstrap objects: %d, %v", count, err)
	}
	var schemaAfter string
	if err := db.QueryRowContext(t.Context(), "SELECT sql FROM sqlite_master WHERE name = 'usertoken'").Scan(&schemaAfter); err != nil || schemaAfter != schema {
		t.Fatalf("failed setup changed the existing table: %q, %v", schemaAfter, err)
	}

	// Rebuild explicitly for this test. Initialization must not perform the repair.
	if _, err := db.ExecContext(t.Context(), accountKeyTestSchema+`;
		INSERT INTO user (id, email, email_key) VALUES ('account', 'person@example.invalid', 'person@example.invalid');
		ALTER TABLE usertoken RENAME TO old_tokens;`+tokenKeyTestSchema+`;
		INSERT INTO usertoken SELECT * FROM old_tokens;
		DROP TABLE old_tokens`); err != nil {
		t.Fatal(err)
	}
	if err := NewAccountRepository(db).Initialize(t.Context()); err != nil {
		t.Fatal("retry after explicit repair:", err)
	}
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM usertoken WHERE user_id = 'account' AND token_hash = 'preserved' AND expires_at = '2030-01-01' AND token_type = 'auth-reset'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("setup changed the token: %d, %v", count, err)
	}
}

func TestTokenForeignKeyInspectionFailures(t *testing.T) {
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
			if err := requireTokenForeignKey(ctx, tx); !errors.Is(err, want) {
				t.Fatalf("inspection lost its underlying failure: %v", err)
			}
		})
	}
}
