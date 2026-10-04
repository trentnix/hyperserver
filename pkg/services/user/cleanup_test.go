package user

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestTokenCleanupBatches(t *testing.T) {
	db := userTestDB(t)
	u := &User{Email: "cleanup@example.invalid"}
	if err := u.Create(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	for i := range 5 {
		token := &AuthToken{UserId: u.ID, TokenHash: fmt.Sprint(i), Type: resetTokenType,
			ExpiresAt: time.Now().Add(-time.Hour).In(time.FixedZone("offset", (i-2)*3600))}
		if i%2 == 0 {
			token.Type = verificationTokenType
		}
		if err := token.Create(db); err != nil {
			t.Fatal(err)
		}
	}
	live := &AuthToken{UserId: u.ID, TokenHash: "live", Type: verificationTokenType, ExpiresAt: time.Now().Add(time.Hour)}
	if err := live.Create(db); err != nil {
		t.Fatal(err)
	}
	// Existing databases receive the cleanup index without losing their tokens.
	if _, err := db.Exec(`DROP INDEX usertoken_expiration`); err != nil {
		t.Fatal(err)
	}
	if err := PrepareDatabase(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	var indexes int
	if err := db.Get(&indexes, `SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = 'usertoken_expiration'`); err != nil || indexes != 1 {
		t.Fatalf("expiration index missing: %d, %v", indexes, err)
	}

	for _, want := range []int64{2, 2, 1, 0} {
		count, err := DeleteExpiredTokens(context.Background(), db, 2)
		if err != nil || count != want {
			t.Fatalf("batch deleted %d, %v, want %d", count, err, want)
		}
	}
	stored, err := GetAuthVerificationTokenByHash(db, live.TokenHash)
	if err != nil || stored.UserId != u.ID || !stored.ExpiresAt.Equal(live.ExpiresAt) {
		t.Fatalf("cleanup changed live token: %+v, %v", stored, err)
	}
	var count int
	if err := db.Get(&count, `SELECT count(*) FROM user WHERE id = ?`, u.ID); err != nil || count != 1 {
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
				if err := db.Get(&remaining, `SELECT count(*) FROM usertoken`); err != nil || remaining != 1 {
					t.Fatalf("failed cleanup changed storage: %d, %v", remaining, err)
				}
			}
		})
	}
}

func TestTokenCleanupDuringRedemption(t *testing.T) {
	for _, purpose := range []string{resetTokenType, verificationTokenType} {
		t.Run(purpose, func(t *testing.T) {
			db := userTestDB(t)
			db.SetMaxOpenConns(4)
			u := &User{Email: "redeem@example.invalid", Password: "original", RegistrationAuthType: "email"}
			if err := u.Create(context.Background(), db); err != nil {
				t.Fatal(err)
			}
			key := []byte("cleanup-test-key")
			token, err := newAuthToken(u, key, time.Hour, purpose)
			if err != nil {
				t.Fatal(err)
			}
			if err := token.Create(db); err != nil {
				t.Fatal(err)
			}
			expired := &AuthToken{UserId: u.ID, TokenHash: "expired", ExpiresAt: time.Now().Add(-time.Hour), Type: purpose}
			if err := expired.Create(db); err != nil {
				t.Fatal(err)
			}

			start := make(chan struct{})
			redeemed := make(chan error, 1)
			go func() {
				<-start
				if purpose == resetTokenType {
					redeemed <- u.ResetPassword(context.Background(), db, token.Token, key, "new hash")
				} else {
					_, err := Verify(context.Background(), db, token.Token, key)
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
			if err := db.Get(&remaining, `SELECT count(*) FROM usertoken`); err != nil || remaining != 0 {
				t.Fatalf("tokens remained: %d, %v", remaining, err)
			}
			stored, err := GetUserByID(db, u.ID)
			if err != nil || (purpose == resetTokenType && stored.Password != "new hash") || (purpose == verificationTokenType && !stored.Verified) {
				t.Fatalf("account operation failed: %+v, %v", stored, err)
			}
		})
	}
}
