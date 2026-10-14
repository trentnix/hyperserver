package sqlite

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	accounts "github.com/trentnix/hyperserver/pkg/services/user"
)

func TestAccountColumnDeclarations(t *testing.T) {
	for _, column := range []struct {
		table, name, declaredType string
		notNull                   bool
	}{
		{"user", "id", "VARCHAR(36)", false},
		{"user", "email", "VARCHAR(255)", true},
		{"user", "email_key", "TEXT", true},
		{"user", "verified", "BOOLEAN", true},
		{"user", "verification_required", "BOOLEAN", true},
		{"user", "created_at", "TIMESTAMP", true},
		{"user", "updated_at", "TIMESTAMP", true},
		{"user", "registration_auth_type", "VARCHAR(50)", false},
		{"user", "password", "VARCHAR(255)", false},
		{"user", "session_version", "INTEGER", true},
		{"usertoken", "user_id", "VARCHAR(36)", false},
		{"usertoken", "token_hash", "VARCHAR(255)", true},
		{"usertoken", "expires_at", "TIMESTAMP", true},
		{"usertoken", "token_type", "VARCHAR(255)", true},
	} {
		for _, change := range []string{"type", "nullability"} {
			if change == "nullability" && !column.notNull {
				continue
			}
			t.Run(column.table+"/"+column.name+"/"+change, func(t *testing.T) {
				db := unpreparedUserTestDB(t)
				schema := accountKeyTestSchema + ";" + tokenKeyTestSchema
				old := "\n\t" + column.name + " " + column.declaredType
				replacement := "\n\t" + column.name + " BLOB"
				if change == "nullability" {
					replacement = old
					old += " NOT NULL"
				}
				if !strings.Contains(schema, old) {
					t.Fatal("fixture does not contain the column being changed")
				}
				schema = strings.Replace(schema, old, replacement, 1)
				if _, err := db.ExecContext(t.Context(), schema); err != nil {
					t.Fatal(err)
				}

				for range 2 {
					err := NewAccountRepository(db).Initialize(t.Context())
					if err == nil || !strings.Contains(err.Error(), column.table+"."+column.name) {
						t.Fatalf("incompatible declaration accepted or poorly identified: %v", err)
					}
					if change == "nullability" && !strings.Contains(err.Error(), "NOT NULL") {
						t.Fatalf("missing nullability diagnosis: %v", err)
					}
				}
			})
		}
	}
}

func TestAccountColumnTypeCompatibility(t *testing.T) {
	for _, tc := range []struct {
		kind     string
		accepted []string
		rejected []string
	}{
		{"text", []string{"TEXT", "varchar(255)", "NCHAR(50)", "CLOB"}, []string{"", "BLOB", "STRING", "CHARINT", "NUMERIC"}},
		{"integer", []string{"INTEGER", "bigint", "INT8"}, []string{"", "REAL", "TEXT", "BOOLEAN", "TIMESTAMP"}},
		{"boolean", []string{"BOOLEAN", "bool", "INTEGER", "TINYINT"}, []string{"", "TEXT", "REAL", "TIMESTAMP"}},
		{"timestamp", []string{"TIMESTAMP", "datetime", "DATE"}, []string{"", "TEXT", "INTEGER", "TIMESTAMP(6)", " TIMESTAMP "}},
	} {
		for _, declaredType := range tc.accepted {
			if !compatibleAccountColumnType(declaredType, tc.kind) {
				t.Errorf("%s rejected compatible %q", tc.kind, declaredType)
			}
		}
		for _, declaredType := range tc.rejected {
			if compatibleAccountColumnType(declaredType, tc.kind) {
				t.Errorf("%s accepted incompatible %q", tc.kind, declaredType)
			}
		}
	}
}

func TestCompatibleAccountColumnTypesRoundTrip(t *testing.T) {
	for _, timestamp := range []string{"TIMESTAMP", "DATETIME", "DATE"} {
		for _, boolean := range []string{"BOOLEAN", "BOOL", "INTEGER"} {
			t.Run(timestamp+"/"+boolean, func(t *testing.T) {
				db := unpreparedUserTestDB(t)
				schema := accountKeyTestSchema + ";" + tokenKeyTestSchema
				for _, replacement := range [][2]string{
					{"VARCHAR(36)", "TEXT"},
					{"VARCHAR(255)", "CLOB"},
					{"VARCHAR(50)", "NCHAR(50)"},
					{"BOOLEAN", boolean},
					{"session_version INTEGER", "session_version BIGINT"},
					{"TIMESTAMP", timestamp},
				} {
					schema = strings.ReplaceAll(schema, replacement[0], replacement[1])
				}
				if _, err := db.ExecContext(t.Context(), schema); err != nil {
					t.Fatal(err)
				}
				repository := NewAccountRepository(db)
				if err := repository.Initialize(t.Context()); err != nil {
					t.Fatal(err)
				}
				account := &accounts.User{
					Email: "Person@example.invalid", Password: "preserved hash",
					Verified: true, VerificationRequired: false, RegistrationAuthType: "email",
				}
				if err := repository.Create(t.Context(), account); err != nil {
					t.Fatal(err)
				}
				stored, err := repository.GetByEmail(t.Context(), "PERSON@EXAMPLE.INVALID")
				if err != nil {
					t.Fatal(err)
				}
				if !stored.CreatedAt.Equal(account.CreatedAt) || !stored.UpdatedAt.Equal(account.UpdatedAt) {
					t.Fatalf("account timestamps changed: %+v, want %+v", stored, account)
				}
				expected := *account
				expected.CreatedAt = stored.CreatedAt
				expected.UpdatedAt = stored.UpdatedAt
				if *stored != expected {
					t.Fatalf("account round trip = %+v, %v, want %+v", stored, err, account)
				}

				token := accounts.TokenMetadata{
					AccountID: account.ID, TokenHash: "01234567890123456789",
					Purpose: accounts.ResetTokenPurpose, ExpiresAt: time.Now().Add(time.Hour).UTC(),
				}
				if err := repository.InsertToken(t.Context(), token); err != nil {
					t.Fatal(err)
				}
				gotToken, err := repository.GetToken(t.Context(), token.TokenHash, token.Purpose)
				if err != nil || gotToken != token {
					t.Fatalf("token round trip = %+v, %v, want %+v", gotToken, err, token)
				}
			})
		}
	}
}

func TestAccountGeneratedColumnsRejected(t *testing.T) {
	for _, storage := range []string{"VIRTUAL", "STORED"} {
		t.Run(storage, func(t *testing.T) {
			db := unpreparedUserTestDB(t)
			schema := strings.Replace(accountKeyTestSchema, "password VARCHAR(255)", "password VARCHAR(255) GENERATED ALWAYS AS (email) "+storage, 1)
			if _, err := db.ExecContext(t.Context(), schema); err != nil {
				t.Fatal(err)
			}
			if err := NewAccountRepository(db).Initialize(t.Context()); err == nil || !strings.Contains(err.Error(), "user.password must be an ordinary writable column") {
				t.Fatalf("generated column accepted: %v", err)
			}
		})
	}
}

func TestAccountColumnFailurePreservesStorage(t *testing.T) {
	db := unpreparedUserTestDB(t)
	schema := strings.Replace(accountKeyTestSchema, "email VARCHAR(255) NOT NULL", "email VARCHAR(255)", 1)
	if _, err := db.ExecContext(t.Context(), schema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), "INSERT INTO user (id, email, email_key, password) VALUES ('existing', NULL, 'stored-key', 'preserved')"); err != nil {
		t.Fatal(err)
	}
	if err := NewAccountRepository(db).Initialize(t.Context()); err == nil {
		t.Fatal("nullable email accepted")
	}
	var count int
	if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM sqlite_master WHERE name IN ('usertoken', 'usertoken_expiration')").Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed setup left bootstrap objects: %d, %v", count, err)
	}
	if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM user WHERE id = 'existing' AND email IS NULL AND password = 'preserved'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("failed setup changed invalid data: %d, %v", count, err)
	}
	var notNull bool
	if err := db.QueryRowContext(t.Context(), `SELECT "notnull" FROM pragma_table_xinfo('user') WHERE name = 'email'`).Scan(&notNull); err != nil || notNull {
		t.Fatalf("failed setup altered the existing declaration: %t, %v", notNull, err)
	}
}

func TestAccountColumnInspectionCancellation(t *testing.T) {
	db := userTestDB(t)
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := validateAccountColumns(ctx, tx); !errors.Is(err, context.Canceled) {
		t.Fatalf("inspection lost cancellation: %v", err)
	}
}
