package user

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
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
	if firstClaims.ID == "" || secondClaims.ID == "" || firstClaims.ID == secondClaims.ID {
		t.Fatal("resend must have a distinct token ID even when issued in the same second")
	}
	if err := second.Create(db); err != nil {
		t.Fatal(err)
	}
	before, err := GetUserByID(db, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := Verify(context.Background(), NewSQLiteAccountRepository(db), second.Token, key)
	if err != nil || verified == nil || !verified.Verified {
		t.Fatalf("verification failed: %v", err)
	}
	stored, err := GetUserByID(db, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	before.Verified, before.UpdatedAt = true, stored.UpdatedAt
	before.SessionVersion++
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
	if got, err := Verify(context.Background(), NewSQLiteAccountRepository(db), second.Token, key); !errors.As(err, &missing) || got != nil {
		t.Fatalf("replayed token accepted: %v", err)
	}
	// Each outstanding token remains independently usable once, including when
	// another link has already verified the same address.
	alreadyVerified, err := Verify(context.Background(), NewSQLiteAccountRepository(db), first.Token, key)
	if err != nil {
		t.Fatalf("earlier delivered link was invalidated by resend: %v", err)
	}
	stored.UpdatedAt = alreadyVerified.UpdatedAt
	if *alreadyVerified != *stored {
		t.Fatal("verifying an already-verified account changed its session version or other fields")
	}
}

func TestVerifyFailureRollsBack(t *testing.T) {
	for _, failure := range []string{"update", "delete", "commit", "email changed", "missing account", "missing token", "stored account", "stored purpose", "expired", "wrong key", "canceled"} {
		t.Run(failure, func(t *testing.T) {
			db, u, token, key := verificationFixture(t)
			ctx := context.Background()
			purpose := verificationTokenType
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
				if _, err := ValidateVerificationToken(context.Background(), NewSQLiteAccountRepository(db), token.Token, key); err == nil {
					t.Fatal("old address token passed validation")
				}
			case "missing account":
				statements = []string{`DELETE FROM user`}
			case "missing token":
				statements = []string{`DELETE FROM usertoken`}
			case "stored account":
				if _, err := db.Exec(`UPDATE usertoken SET user_id = ? WHERE token_hash = ?`, "another-account", token.TokenHash); err != nil {
					t.Fatal(err)
				}
			case "stored purpose":
				purpose = resetTokenType
				if _, err := db.Exec(`UPDATE usertoken SET token_type = ? WHERE token_hash = ?`, purpose, token.TokenHash); err != nil {
					t.Fatal(err)
				}
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
			if got, err := Verify(ctx, NewSQLiteAccountRepository(db), token.Token, key); err == nil || got != nil {
				t.Fatal("verification succeeded despite failure")
			}
			if failure != "missing account" {
				stored, err := GetUserByID(db, u.ID)
				if err != nil || *stored != *before {
					t.Fatalf("failed verification changed the account: %v", err)
				}
			}
			if failure != "missing token" {
				if _, err := getTokenByHash(db, token.TokenHash, purpose); err != nil {
					t.Fatalf("failed verification consumed token: %v", err)
				}
			}
			if failure == "update" {
				if _, err := db.Exec(`DROP TRIGGER fail_verify`); err != nil {
					t.Fatal(err)
				}
				if _, err := Verify(ctx, NewSQLiteAccountRepository(db), token.Token, key); err != nil {
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
			_, err := Verify(ctx, NewSQLiteAccountRepository(observed), token.Token, key)
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
	if successes != 1 || err != nil || !stored.Verified || stored.SessionVersion != u.SessionVersion+1 {
		t.Fatalf("concurrent verification: successes=%d, lookup error=%v", successes, err)
	}
}

func TestSQLiteAccountRepositoryVerifyRejectsInvalidInput(t *testing.T) {
	for _, scenario := range []string{"nil database", "missing account", "missing email", "missing hash", "missing expiry", "expired authorization"} {
		t.Run(scenario, func(t *testing.T) {
			db, u, token, _ := verificationFixture(t)
			repository := NewSQLiteAccountRepository(db)
			before, err := repository.GetByID(context.Background(), u.ID)
			if err != nil {
				t.Fatal(err)
			}
			authorization := VerificationAuthorization{AccountID: u.ID, Email: u.Email, TokenHash: token.TokenHash, ExpiresAt: time.Unix(token.ExpiresAt.Unix(), 0)}
			switch scenario {
			case "nil database":
				repository = NewSQLiteAccountRepository(nil)
			case "missing account":
				authorization.AccountID = ""
			case "missing email":
				authorization.Email = ""
			case "missing hash":
				authorization.TokenHash = ""
			case "missing expiry":
				authorization.ExpiresAt = time.Time{}
			case "expired authorization":
				authorization.ExpiresAt = time.Now().Add(-time.Hour)
			}
			if got, err := repository.Verify(context.Background(), authorization); got != nil || err == nil {
				t.Fatalf("invalid verification succeeded: %v, %v", got, err)
			}
			stored, err := GetUserByID(db, u.ID)
			if err != nil || *stored != *before {
				t.Fatalf("invalid verification changed storage: %v", err)
			}
			if _, err := GetAuthVerificationTokenByHash(db, token.TokenHash); err != nil {
				t.Fatalf("invalid verification consumed token: %v", err)
			}
		})
	}
}

func TestVerifyCancellationAndExpiryDuringContention(t *testing.T) {
	for _, scenario := range []string{"cancellation", "signed expiry", "stored expiry"} {
		t.Run(scenario, func(t *testing.T) {
			db, u, token, _ := verificationFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			before, err := GetUserByID(db, u.ID)
			if err != nil {
				t.Fatal(err)
			}
			// Exercise storage after signature validation with a short deadline.
			authorization := VerificationAuthorization{AccountID: u.ID, Email: u.Email, TokenHash: token.TokenHash, ExpiresAt: time.Unix(token.ExpiresAt.Unix(), 0)}
			deadline := time.Now().Add(2 * time.Second)
			if scenario == "signed expiry" {
				authorization.ExpiresAt = deadline
			} else if scenario == "stored expiry" {
				if _, err := db.Exec(`UPDATE usertoken SET expires_at = ? WHERE token_hash = ?`, deadline, token.TokenHash); err != nil {
					t.Fatal(err)
				}
			}
			observed, started := observeTokenDeletes(t, db)
			lock, err := db.BeginTxx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Rollback()
			if _, err := lock.ExecContext(ctx, `UPDATE user SET verified = verified WHERE id = ?`, u.ID); err != nil {
				t.Fatal(err)
			}

			verifyCtx, cancelVerify := context.WithCancel(ctx)
			defer cancelVerify()
			type result struct {
				account *User
				err     error
			}
			results := make(chan result, 1)
			go func() {
				account, err := NewSQLiteAccountRepository(observed).Verify(verifyCtx, authorization)
				results <- result{account, err}
			}()
			waitForTokenDelete(t, ctx, started)
			if scenario == "cancellation" {
				cancelVerify()
			} else {
				if !time.Now().Before(deadline) {
					t.Fatal("token expired before contention was established")
				}
				timer := time.NewTimer(time.Until(deadline))
				defer timer.Stop()
				select {
				case <-timer.C:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			if err := lock.Rollback(); err != nil {
				t.Fatal(err)
			}
			var outcome result
			select {
			case outcome = <-results:
			case <-ctx.Done():
				t.Fatal("verification did not finish:", ctx.Err())
			}
			if outcome.account != nil {
				t.Fatal("failed verification returned an account")
			}
			if scenario == "cancellation" {
				if !errors.Is(outcome.err, context.Canceled) {
					t.Fatalf("verification error=%v, want context.Canceled", outcome.err)
				}
			} else {
				var expired *ErrTokenExpired
				if !errors.As(outcome.err, &expired) {
					t.Fatalf("verification error=%v, want ErrTokenExpired", outcome.err)
				}
			}
			stored, err := GetUserByID(db, u.ID)
			if err != nil || *stored != *before {
				t.Fatalf("failed verification changed the account: %v", err)
			}
			if _, err := GetAuthVerificationTokenByHash(db, token.TokenHash); err != nil {
				t.Fatalf("failed verification consumed the token: %v", err)
			}
		})
	}
}

type verificationAccountRepository struct {
	AccountRepository
	verify func(context.Context, VerificationAuthorization) (*User, error)
}

func (s verificationAccountRepository) Verify(ctx context.Context, authorization VerificationAuthorization) (*User, error) {
	return s.verify(ctx, authorization)
}

func TestVerifyValidatesBeforeCallingRepository(t *testing.T) {
	for _, scenario := range []string{"success", "storage failure", "canceled", "wrong signature", "wrong purpose", "missing account", "missing email", "expired", "malformed", "nil repository"} {
		t.Run(scenario, func(t *testing.T) {
			account := &User{ID: "account", Email: "person@example.invalid", Verified: true, SessionVersion: 2}
			key := []byte("test-signing-key")
			claims := &VerificationClaims{Id: account.ID, Email: account.Email, Purpose: verificationTokenType,
				RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}}
			switch scenario {
			case "wrong purpose":
				claims.Purpose = resetTokenType
			case "missing account":
				claims.Id = ""
			case "missing email":
				claims.Email = ""
			case "expired":
				claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Hour))
			}
			raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(key)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "wrong signature" {
				key = []byte("wrong-key")
			} else if scenario == "malformed" {
				raw = "not-a-token"
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var wantErr error
			if scenario == "storage failure" {
				wantErr = errors.New("storage unavailable")
			} else if scenario == "canceled" {
				cancel()
				wantErr = context.Canceled
			}
			calls := 0
			hash := sha256.Sum256([]byte(raw))
			var repository AccountRepository = verificationAccountRepository{verify: func(got context.Context, authorization VerificationAuthorization) (*User, error) {
				calls++
				if got != ctx || authorization.AccountID != account.ID || authorization.Email != account.Email || authorization.TokenHash != hex.EncodeToString(hash[:]) || !authorization.ExpiresAt.Equal(claims.ExpiresAt.Time) {
					t.Fatal("verification lost its context or signed authorization")
				}
				if wantErr != nil {
					return nil, wantErr
				}
				return account, nil
			}}
			if scenario == "nil repository" {
				repository = nil
			}
			got, err := Verify(ctx, repository, raw, key)
			if scenario == "success" {
				if err != nil || calls != 1 || got != account {
					t.Fatalf("verification result=%v, error=%v, calls=%d", got, err, calls)
				}
			} else if wantErr != nil {
				if got != nil || calls != 1 || !errors.Is(err, wantErr) {
					t.Fatalf("verification lost repository failure: result=%v, error=%v, calls=%d", got, err, calls)
				}
			} else if got != nil || err == nil || calls != 0 {
				t.Fatalf("invalid verification reached storage: result=%v, error=%v, calls=%d", got, err, calls)
			}
		})
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
	if err := NewSQLiteAccountRepository(db).Update(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	for _, token := range []*AuthVerificationToken{first, second} {
		if _, err := ValidateVerificationToken(context.Background(), NewSQLiteAccountRepository(db), token.Token, key); err != nil {
			t.Fatalf("unchanged email invalidated verification: %v", err)
		}
	}

	originalEmail := u.Email
	for _, email := range []string{"changed@example.invalid", originalEmail} {
		u.Email = email
		if err := NewSQLiteAccountRepository(db).Update(context.Background(), u); err != nil {
			t.Fatal(err)
		}
		for _, token := range []*AuthVerificationToken{first, second} {
			var missing *ErrTokenNotFound
			if _, err := Verify(context.Background(), NewSQLiteAccountRepository(db), token.Token, key); !errors.As(err, &missing) {
				t.Fatalf("old link at %s: got %v, want revoked token", email, err)
			}
		}
	}
	if _, err := GetAuthResetTokenByHash(db, reset.TokenHash); err != nil {
		t.Fatalf("email change revoked a reset token: %v", err)
	}
	if _, err := ValidateVerificationToken(context.Background(), NewSQLiteAccountRepository(db), otherToken.Token, key); err != nil {
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
	if _, err := Verify(context.Background(), NewSQLiteAccountRepository(db), newToken.Token, key); err != nil {
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
			if err := NewSQLiteAccountRepository(db).Update(context.Background(), u); err == nil {
				t.Fatal("email change succeeded despite storage failure")
			}
			if *u != attempt {
				t.Fatal("failed email change mutated the receiver")
			}
			stored, err := GetUserByID(db, u.ID)
			if err != nil || *stored != *before {
				t.Fatalf("failed email change mutated the account: %v", err)
			}
			if _, err := ValidateVerificationToken(context.Background(), NewSQLiteAccountRepository(db), token.Token, key); err != nil {
				t.Fatalf("failed email change revoked the verification link: %v", err)
			}
		})
	}
}
