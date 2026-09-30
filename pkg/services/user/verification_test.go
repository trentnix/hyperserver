package user

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

func verificationFixture(t *testing.T) (*sqlx.DB, *User, *AuthVerificationToken, []byte) {
	t.Helper()
	db := userTestDB(t)
	u := &User{Email: "person@example.invalid", Password: "original hash", VerificationRequired: true, RegistrationAuthType: "email"}
	if err := u.Create(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	key := []byte("test-verification-key")
	token, err := NewAuthVerificationToken(u, key, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := token.Create(db); err != nil {
		t.Fatal(err)
	}
	return db, u, token, key
}

func TestVerifyConsumesExactTokenAndRejectsReplay(t *testing.T) {
	db, u, first, key := verificationFixture(t)
	second, err := NewAuthVerificationToken(u, key, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if second.Token == first.Token {
		t.Fatal("resend produced the same token")
	}
	firstClaims, err := parseAuthToken(first.Token, key, verificationTokenType)
	if err != nil {
		t.Fatal(err)
	}
	secondClaims, err := parseAuthToken(second.Token, key, verificationTokenType)
	if err != nil {
		t.Fatal(err)
	}
	if firstClaims.StandardClaims.Id == "" || secondClaims.StandardClaims.Id == "" || firstClaims.StandardClaims.Id == secondClaims.StandardClaims.Id {
		t.Fatal("resend must have a distinct token ID even when issued in the same second")
	}
	if err := second.Create(db); err != nil {
		t.Fatal(err)
	}
	before, err := GetUserByID(db, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := Verify(context.Background(), db, second.Token, key)
	if err != nil || verified == nil || !verified.Verified {
		t.Fatalf("verification failed: %v", err)
	}
	stored, err := GetUserByID(db, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	before.Verified, before.UpdatedAt = true, stored.UpdatedAt
	if *stored != *before || *verified != *stored {
		t.Fatal("verification changed unrelated account data")
	}
	var missing *ErrTokenNotFound
	if _, err := GetAuthVerificationTokenByHash(db, second.TokenHash); !errors.As(err, &missing) {
		t.Fatalf("presented token was not consumed: %v", err)
	}
	if _, err := GetAuthVerificationTokenByHash(db, first.TokenHash); err != nil {
		t.Fatalf("different token was consumed: %v", err)
	}
	if got, err := Verify(context.Background(), db, second.Token, key); !errors.As(err, &missing) || got != nil {
		t.Fatalf("replayed token accepted: %v", err)
	}
	// Each outstanding token remains independently usable once, including when
	// another link has already verified the same address.
	if _, err := Verify(context.Background(), db, first.Token, key); err != nil {
		t.Fatalf("earlier delivered link was invalidated by resend: %v", err)
	}
}

func TestVerifyFailureRollsBack(t *testing.T) {
	for _, failure := range []string{"update", "delete", "commit", "email changed", "missing account", "missing token", "expired", "wrong key", "canceled"} {
		t.Run(failure, func(t *testing.T) {
			db, u, token, key := verificationFixture(t)
			ctx := context.Background()
			var statements []string
			switch failure {
			case "update":
				statements = []string{`CREATE TRIGGER fail_verify BEFORE UPDATE ON user BEGIN SELECT RAISE(ABORT, 'failed update'); END`}
			case "delete":
				statements = []string{`CREATE TRIGGER fail_verify BEFORE DELETE ON usertoken BEGIN SELECT RAISE(ABORT, 'failed deletion'); END`}
			case "commit":
				statements = []string{
					`PRAGMA foreign_keys = ON`,
					`CREATE TABLE verification_failure (user_id TEXT REFERENCES user(id) DEFERRABLE INITIALLY DEFERRED)`,
					`CREATE TRIGGER fail_verify AFTER UPDATE ON user BEGIN INSERT INTO verification_failure VALUES ('missing'); END`,
				}
			case "email changed":
				// Bypass Update's revocation to test the signed email check itself.
				if _, err := db.Exec(`UPDATE user SET email = ? WHERE id = ?`, "changed@example.invalid", u.ID); err != nil {
					t.Fatal(err)
				}
				if _, err := ValidateVerificationToken(db, token.Token, key); err == nil {
					t.Fatal("old address token passed validation")
				}
			case "missing account":
				statements = []string{`DELETE FROM user`}
			case "missing token":
				statements = []string{`DELETE FROM usertoken`}
			case "expired":
				if _, err := db.Exec(`UPDATE usertoken SET expires_at = ?`, time.Now().Add(-time.Hour)); err != nil {
					t.Fatal(err)
				}
			case "wrong key":
				key = []byte("wrong-key")
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			for _, statement := range statements {
				if _, err := db.Exec(statement); err != nil {
					t.Fatal(err)
				}
			}
			before, err := GetUserByID(db, u.ID)
			if failure != "missing account" && err != nil {
				t.Fatal(err)
			}
			if got, err := Verify(ctx, db, token.Token, key); err == nil || got != nil {
				t.Fatal("verification succeeded despite failure")
			}
			if failure != "missing account" {
				stored, err := GetUserByID(db, u.ID)
				if err != nil || *stored != *before {
					t.Fatalf("failed verification changed the account: %v", err)
				}
			}
			if failure != "missing token" {
				if _, err := GetAuthVerificationTokenByHash(db, token.TokenHash); err != nil {
					t.Fatalf("failed verification consumed token: %v", err)
				}
			}
			if failure == "update" {
				if _, err := db.Exec(`DROP TRIGGER fail_verify`); err != nil {
					t.Fatal(err)
				}
				if _, err := Verify(ctx, db, token.Token, key); err != nil {
					t.Fatalf("retry failed: %v", err)
				}
			}
		})
	}
}

func TestVerifyConcurrentRedemption(t *testing.T) {
	db, u, token, key := verificationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	observed, started := observeTokenDeletes(t, db)
	lock, err := db.BeginTxx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback()
	if _, err := lock.ExecContext(ctx, `UPDATE user SET password = password WHERE id = ?`, u.ID); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := Verify(ctx, observed, token.Token, key)
			results <- err
		}()
	}
	for range 2 {
		waitForTokenDelete(t, ctx, started)
	}
	select {
	case err := <-results:
		t.Fatalf("verification finished while the write lock was held: %v", err)
	default:
	}
	if err := lock.Rollback(); err != nil {
		t.Fatal(err)
	}
	successes := 0
	for range 2 {
		select {
		case err := <-results:
			if err == nil {
				successes++
			} else {
				var missing *ErrTokenNotFound
				if !errors.As(err, &missing) {
					t.Errorf("losing verification returned %v, want ErrTokenNotFound", err)
				}
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	stored, err := GetUserByID(db, u.ID)
	if successes != 1 || err != nil || !stored.Verified {
		t.Fatalf("concurrent verification: successes=%d, lookup error=%v", successes, err)
	}
}

func TestEmailChangeRevokesStoredVerificationTokens(t *testing.T) {
	db, u, first, key := verificationFixture(t)
	second, err := NewAuthVerificationToken(u, key, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Create(db); err != nil {
		t.Fatal(err)
	}
	reset, err := NewAuthResetToken(u, key, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := reset.Create(db); err != nil {
		t.Fatal(err)
	}
	other := &User{Email: "other@example.invalid"}
	if err := other.Create(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	otherToken, err := NewAuthVerificationToken(other, key, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := otherToken.Create(db); err != nil {
		t.Fatal(err)
	}

	// Updating unrelated fields must preserve both outstanding verification links.
	u.Password = "new hash"
	if err := u.Update(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	for _, token := range []*AuthVerificationToken{first, second} {
		if _, err := ValidateVerificationToken(db, token.Token, key); err != nil {
			t.Fatalf("unchanged email invalidated verification: %v", err)
		}
	}

	originalEmail := u.Email
	for _, email := range []string{"changed@example.invalid", originalEmail} {
		u.Email = email
		if err := u.Update(context.Background(), db); err != nil {
			t.Fatal(err)
		}
		for _, token := range []*AuthVerificationToken{first, second} {
			var missing *ErrTokenNotFound
			if _, err := Verify(context.Background(), db, token.Token, key); !errors.As(err, &missing) {
				t.Fatalf("old link at %s: got %v, want revoked token", email, err)
			}
		}
	}
	if _, err := GetAuthResetTokenByHash(db, reset.TokenHash); err != nil {
		t.Fatalf("email change revoked a reset token: %v", err)
	}
	if _, err := ValidateVerificationToken(db, otherToken.Token, key); err != nil {
		t.Fatalf("email change revoked another account's token: %v", err)
	}
	stored, err := GetUserByID(db, u.ID)
	if err != nil || stored.Verified {
		t.Fatalf("revoked link verified the account: %v", err)
	}
	newToken, err := NewAuthVerificationToken(u, key, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := newToken.Create(db); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(context.Background(), db, newToken.Token, key); err != nil {
		t.Fatalf("fresh link was rejected: %v", err)
	}
}

func TestEmailChangeRevocationRollsBack(t *testing.T) {
	for _, failure := range []string{"delete", "update", "commit", "email conflict"} {
		t.Run(failure, func(t *testing.T) {
			db, u, token, key := verificationFixture(t)
			before, err := GetUserByID(db, u.ID)
			if err != nil {
				t.Fatal(err)
			}
			var statements []string
			switch failure {
			case "delete":
				statements = []string{`CREATE TRIGGER fail_email_change BEFORE DELETE ON usertoken BEGIN SELECT RAISE(ABORT, 'failed deletion'); END`}
			case "update":
				statements = []string{`CREATE TRIGGER fail_email_change BEFORE UPDATE ON user BEGIN SELECT RAISE(ABORT, 'failed update'); END`}
			case "commit":
				statements = []string{
					`PRAGMA foreign_keys = ON`,
					`CREATE TABLE email_change_failure (user_id TEXT REFERENCES user(id) DEFERRABLE INITIALLY DEFERRED)`,
					`CREATE TRIGGER fail_email_change AFTER UPDATE ON user BEGIN INSERT INTO email_change_failure VALUES ('missing'); END`,
				}
			case "email conflict":
				other := &User{Email: "changed@example.invalid"}
				if err := other.Create(context.Background(), db); err != nil {
					t.Fatal(err)
				}
			}
			for _, statement := range statements {
				if _, err := db.Exec(statement); err != nil {
					t.Fatal(err)
				}
			}

			u.Email = "changed@example.invalid"
			attempt := *u
			if err := u.Update(context.Background(), db); err == nil {
				t.Fatal("email change succeeded despite storage failure")
			}
			if *u != attempt {
				t.Fatal("failed email change mutated the receiver")
			}
			stored, err := GetUserByID(db, u.ID)
			if err != nil || *stored != *before {
				t.Fatalf("failed email change mutated the account: %v", err)
			}
			if _, err := ValidateVerificationToken(db, token.Token, key); err != nil {
				t.Fatalf("failed email change revoked the verification link: %v", err)
			}
		})
	}
}
