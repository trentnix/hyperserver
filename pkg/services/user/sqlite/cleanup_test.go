package sqlite

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	accounts "github.com/trentnix/hyperserver/pkg/services/user"
)

func TestOpenSQLiteTokenMaintenance(t *testing.T) {
	for _, scenario := range []string{"success", "missing file", "canceled", "empty connection", "invalid connection", "unavailable", "foreign keys disabled"} {
		t.Run(scenario, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "accounts.db")
			if scenario == "success" {
				if err := os.WriteFile(path, nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			connection := path + "?_timeout=75"
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch scenario {
			case "canceled":
				cancel()
			case "empty connection":
				connection = ""
			case "invalid connection":
				connection += "&_busy_timeout=invalid"
			case "unavailable":
				connection = filepath.Join(t.TempDir(), "missing", "accounts.db")
			case "foreign keys disabled":
				connection += "&_fk=false"
			}
			maintenance, closeStore, err := OpenTokenMaintenance(ctx, connection)
			if scenario != "success" {
				if err == nil || maintenance != nil || closeStore != nil {
					t.Fatalf("failed opening returned resources: %v, %v", maintenance, err)
				}
				if scenario == "canceled" && !errors.Is(err, context.Canceled) {
					t.Fatal("lost cancellation:", err)
				}
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("failed maintenance created a database: %v", err)
				}
				return
			}
			if err != nil || closeStore == nil {
				t.Fatal("opening failed:", err)
			}
			t.Cleanup(func() { _ = closeStore() })
			repository := maintenance.(*AccountRepository)
			if repository.db.Stats().MaxOpenConnections != 1 {
				t.Fatal("maintenance pool must limit open connections")
			}
			for pragma, want := range map[string]int{"foreign_keys": 1, "busy_timeout": 75} {
				var got int
				if err := repository.db.QueryRowContext(ctx, "PRAGMA "+pragma).Scan(&got); err != nil || got != want {
					t.Fatalf("%s = %d, %v, want %d", pragma, got, err, want)
				}
			}
			assertNoAccountTables(t, repository.db)
			if _, err := maintenance.DeleteExpiredTokens(ctx, 1); err == nil {
				t.Fatal("maintenance accepted missing schema")
			}
			assertNoAccountTables(t, repository.db)
			if err := closeStore(); err != nil {
				t.Fatal(err)
			}
			if err := repository.db.Ping(); err == nil {
				t.Fatal("maintenance left its pool open")
			}
		})
	}
}

func TestTokenCleanupBatches(t *testing.T) {
	db := userTestDB(t)
	u := &accounts.User{Email: "cleanup@example.invalid"}
	if err := NewAccountRepository(db).Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	for i := range 5 {
		token := &accounts.AuthToken{UserId: u.ID, TokenHash: fmt.Sprint(i), Type: accounts.ResetTokenPurpose,
			ExpiresAt: time.Now().Add(-time.Hour).In(time.FixedZone("offset", (i-2)*3600))}
		if i%2 == 0 {
			token.Type = accounts.VerificationTokenPurpose
		}
		if err := NewAccountRepository(db).InsertToken(t.Context(), token.Metadata()); err != nil {
			t.Fatal(err)
		}
	}
	live := &accounts.AuthToken{UserId: u.ID, TokenHash: "live", Type: accounts.VerificationTokenPurpose, ExpiresAt: time.Now().Add(time.Hour)}
	if err := NewAccountRepository(db).InsertToken(t.Context(), live.Metadata()); err != nil {
		t.Fatal(err)
	}
	// Repeated initialization preserves the cleanup index and stored tokens.
	if err := PrepareDatabase(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	var indexes int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = 'usertoken_expiration'`).Scan(&indexes); err != nil || indexes != 1 {
		t.Fatalf("expiration index missing: %d, %v", indexes, err)
	}

	for _, want := range []int64{2, 2, 1, 0} {
		count, err := DeleteExpiredTokens(context.Background(), db, 2)
		if err != nil || count != want {
			t.Fatalf("batch deleted %d, %v, want %d", count, err, want)
		}
	}
	stored, err := NewAccountRepository(db).GetToken(t.Context(), live.TokenHash, accounts.VerificationTokenPurpose)
	if err != nil || stored.AccountID != u.ID || !stored.ExpiresAt.Equal(live.ExpiresAt) {
		t.Fatalf("cleanup changed live token: %+v, %v", stored, err)
	}
	var count int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM user WHERE id = ?`, u.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("cleanup removed account: %d, %v", count, err)
	}
}

func TestTokenCleanupFailures(t *testing.T) {
	for _, failure := range []string{"zero limit", "negative limit", "canceled", "delete", "missing table", "closed database", "missing database"} {
		t.Run(failure, func(t *testing.T) {
			db := userTestDB(t)
			if _, err := db.Exec(`INSERT INTO usertoken (user_id, token_hash, token_type, expires_at) VALUES ('user', 'hash', 'auth-reset', ?)`, time.Now().Add(-time.Hour)); err != nil {
				t.Fatal(err)
			}
			ctx, limit := context.Background(), 10
			input := db
			switch failure {
			case "zero limit":
				limit = 0
			case "negative limit":
				limit = -1
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "delete":
				if _, err := db.Exec(`CREATE TRIGGER reject_cleanup BEFORE DELETE ON usertoken BEGIN SELECT RAISE(ABORT, 'cleanup failed'); END`); err != nil {
					t.Fatal(err)
				}
			case "missing table":
				if _, err := db.Exec(`ALTER TABLE usertoken RENAME TO unavailable_tokens`); err != nil {
					t.Fatal(err)
				}
			case "closed database":
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
			case "missing database":
				input = nil
			}
			count, err := DeleteExpiredTokens(ctx, input, limit)
			if err == nil || count != 0 {
				t.Fatalf("failed cleanup = %d, %v", count, err)
			}
			if failure == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation not preserved: %v", err)
			}
			if failure != "missing table" && failure != "closed database" {
				var remaining int
				if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM usertoken`).Scan(&remaining); err != nil || remaining != 1 {
					t.Fatalf("failed cleanup changed storage: %d, %v", remaining, err)
				}
			}
		})
	}
}

func TestTokenCleanupDuringRedemption(t *testing.T) {
	for _, purpose := range []string{accounts.ResetTokenPurpose, accounts.VerificationTokenPurpose} {
		t.Run(purpose, func(t *testing.T) {
			db := userTestDB(t)
			db.SetMaxOpenConns(4)
			u := &accounts.User{Email: "redeem@example.invalid", Password: "original", RegistrationAuthType: "email"}
			if err := NewAccountRepository(db).Create(context.Background(), u); err != nil {
				t.Fatal(err)
			}
			key := []byte("cleanup-test-key")
			token, err := newTestToken(u, key, time.Hour, purpose)
			if err != nil {
				t.Fatal(err)
			}
			if err := NewAccountRepository(db).InsertToken(t.Context(), token.Metadata()); err != nil {
				t.Fatal(err)
			}
			expired := &accounts.AuthToken{UserId: u.ID, TokenHash: "expired", ExpiresAt: time.Now().Add(-time.Hour), Type: purpose}
			if err := NewAccountRepository(db).InsertToken(t.Context(), expired.Metadata()); err != nil {
				t.Fatal(err)
			}

			start := make(chan struct{})
			redeemed := make(chan error, 1)
			go func() {
				<-start
				if purpose == accounts.ResetTokenPurpose {
					redeemed <- u.ResetPassword(context.Background(), NewAccountRepository(db), token.Token, key, "new hash")
				} else {
					_, err := accounts.Verify(context.Background(), NewAccountRepository(db), token.Token, key)
					redeemed <- err
				}
			}()
			close(start)
			count, cleanupErr := DeleteExpiredTokens(context.Background(), db, 1)
			if err := <-redeemed; err != nil {
				t.Fatal("cleanup interfered with redemption:", err)
			}
			if cleanupErr != nil || count != 1 {
				t.Fatalf("concurrent cleanup = %d, %v", count, cleanupErr)
			}
			var remaining int
			if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM usertoken`).Scan(&remaining); err != nil || remaining != 0 {
				t.Fatalf("tokens remained: %d, %v", remaining, err)
			}
			stored, err := NewAccountRepository(db).GetByID(t.Context(), u.ID)
			if err != nil || (purpose == accounts.ResetTokenPurpose && stored.Password != "new hash") || (purpose == accounts.VerificationTokenPurpose && !stored.Verified) {
				t.Fatalf("account operation failed: %+v, %v", stored, err)
			}
		})
	}
}
