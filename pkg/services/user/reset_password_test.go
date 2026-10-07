package user

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestResetPasswordConsumesExactToken(t *testing.T) {
	db := userTestDB(t)
	ctx := context.Background()
	u := &User{Email: "person@example.invalid", Password: "old hash", RegistrationAuthType: "email"}
	if err := u.Create(ctx, db); err != nil {
		t.Fatal(err)
	}
	key := []byte("test-signing-key")
	var tokens []*AuthResetToken
	for _, lifetime := range []time.Duration{time.Hour, 2 * time.Hour} {
		token, err := NewAuthResetToken(u, key, lifetime)
		if err != nil {
			t.Fatal(err)
		}
		if err := token.Create(db); err != nil {
			t.Fatal(err)
		}
		tokens = append(tokens, token)
	}
	original, err := GetUserByID(db, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := u.ResetPassword(ctx, NewSQLiteAccountRepository(db), tokens[1].Token, key, "new hash"); err != nil {
		t.Fatal(err)
	}
	if _, err := GetAuthResetTokenByHash(db, tokens[0].TokenHash); err != nil {
		t.Fatalf("consumed a different token: %v", err)
	}
	var missing *ErrTokenNotFound
	if _, err := GetAuthResetTokenByHash(db, tokens[1].TokenHash); !errors.As(err, &missing) {
		t.Fatalf("presented token still exists: %v", err)
	}
	if err := u.ResetPassword(ctx, NewSQLiteAccountRepository(db), tokens[1].Token, key, "replayed hash"); !errors.As(err, &missing) {
		t.Fatalf("replay error = %v, want ErrTokenNotFound", err)
	}
	stored, err := GetUserByID(db, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	original.Password = "new hash"
	original.SessionVersion++
	original.UpdatedAt = stored.UpdatedAt
	if *stored != *original || u.Password != stored.Password || !u.UpdatedAt.Equal(stored.UpdatedAt) {
		t.Fatal("reset changed unrelated account data or replay changed the password")
	}
}

func TestResetPasswordFailureLeavesAccountAndTokenUnchanged(t *testing.T) {
	for _, failure := range []string{"update", "delete", "commit", "missing account", "stale password", "stale provider", "wrong account", "wrong purpose", "wrong signature", "signed expiry", "stored expiry", "stored account", "stored purpose", "missing token", "canceled", "empty password"} {
		t.Run(failure, func(t *testing.T) {
			db := userTestDB(t)
			ctx := context.Background()
			u := &User{Email: "person@example.invalid", Password: "old hash", RegistrationAuthType: "email"}
			if err := u.Create(ctx, db); err != nil {
				t.Fatal(err)
			}
			key := []byte("test-signing-key")
			purpose, lifetime := resetTokenType, time.Hour
			if failure == "wrong purpose" {
				purpose = verificationTokenType
			}
			if failure == "signed expiry" {
				lifetime = time.Nanosecond
			}
			token, err := newAuthToken(u, key, lifetime, purpose)
			if err != nil {
				t.Fatal(err)
			}
			if failure == "stored expiry" {
				token.ExpiresAt = time.Now().Add(-time.Hour)
			}
			if err := token.Create(db); err != nil {
				t.Fatal(err)
			}
			switch failure {
			case "commit":
				// The trigger succeeds, but its deferred foreign key fails at commit.
				for _, statement := range []string{
					`PRAGMA foreign_keys = ON`,
					`CREATE TABLE reset_failure (user_id TEXT REFERENCES user(id) DEFERRABLE INITIALLY DEFERRED)`,
					`CREATE TRIGGER fail_reset AFTER UPDATE ON user BEGIN INSERT INTO reset_failure VALUES ('missing'); END`,
				} {
					if _, err := db.Exec(statement); err != nil {
						t.Fatal(err)
					}
				}
			case "update", "delete":
				statement := `CREATE TRIGGER fail_reset BEFORE UPDATE ON user BEGIN SELECT RAISE(ABORT, 'failed update'); END`
				if failure == "delete" {
					statement = `CREATE TRIGGER fail_reset BEFORE DELETE ON usertoken BEGIN SELECT RAISE(ABORT, 'failed deletion'); END`
				}
				if _, err := db.Exec(statement); err != nil {
					t.Fatal(err)
				}
			case "missing account":
				if _, err := db.Exec(`DELETE FROM user WHERE id = ?`, u.ID); err != nil {
					t.Fatal(err)
				}
			case "stale password":
				u.Password = "stale hash"
			case "stale provider":
				u.RegistrationAuthType = "another-provider"
			case "wrong account":
				u.ID = "another-account"
			case "wrong signature":
				key = []byte("wrong-key")
			case "stored account":
				if _, err := db.Exec(`UPDATE usertoken SET user_id = ? WHERE token_hash = ?`, "another-account", token.TokenHash); err != nil {
					t.Fatal(err)
				}
			case "stored purpose":
				purpose = verificationTokenType
				if _, err := db.Exec(`UPDATE usertoken SET token_type = ? WHERE token_hash = ?`, purpose, token.TokenHash); err != nil {
					t.Fatal(err)
				}
			case "missing token":
				if err := token.Delete(db); err != nil {
					t.Fatal(err)
				}
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			before := *u
			storedBefore, lookupErr := GetUserByID(db, token.UserId)
			if failure != "missing account" && lookupErr != nil {
				t.Fatal(lookupErr)
			}
			hash := "new hash"
			if failure == "empty password" {
				hash = ""
			}
			if err := u.ResetPassword(ctx, NewSQLiteAccountRepository(db), token.Token, key, hash); err == nil {
				t.Fatal("reset succeeded despite failure")
			}
			if *u != before {
				t.Error("failed reset changed the caller's account")
			}
			if failure != "missing account" {
				stored, err := GetUserByID(db, token.UserId)
				if err != nil || *stored != *storedBefore {
					t.Fatalf("failed reset changed stored account: %v", err)
				}
			}
			if failure != "missing token" {
				if _, err := getTokenByHash(db, token.TokenHash, purpose); err != nil {
					t.Fatalf("failed reset consumed token: %v", err)
				}
			}
			if failure == "update" {
				if _, err := db.Exec(`DROP TRIGGER fail_reset`); err != nil {
					t.Fatal(err)
				}
				if err := u.ResetPassword(ctx, NewSQLiteAccountRepository(db), token.Token, key, hash); err != nil {
					t.Fatalf("retry after rollback failed: %v", err)
				}
			}
		})
	}
}

func TestResetPasswordConcurrentRedemption(t *testing.T) {
	db := userTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	u := &User{Email: "person@example.invalid", Password: "old hash", RegistrationAuthType: "email"}
	if err := u.Create(ctx, db); err != nil {
		t.Fatal(err)
	}
	key := []byte("test-signing-key")
	token, err := NewAuthResetToken(u, key, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := token.Create(db); err != nil {
		t.Fatal(err)
	}
	observed, started := observeTokenDeletes(t, db)
	lock, err := db.BeginTxx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback()
	if _, err := lock.ExecContext(ctx, `UPDATE user SET password = password WHERE id = ?`, u.ID); err != nil {
		t.Fatal(err)
	}

	type result struct {
		hash string
		err  error
	}
	results := make(chan result, 2)
	for i := range 2 {
		account := *u
		go func() {
			hash := fmt.Sprintf("new hash %d", i)
			results <- result{hash, account.ResetPassword(ctx, NewSQLiteAccountRepository(observed), token.Token, key, hash)}
		}()
	}
	// Both transactions must reach deletion while a separate connection owns
	// the write lock. Neither redemption can finish before the lock is released.
	for range 2 {
		waitForTokenDelete(t, ctx, started)
	}
	select {
	case result := <-results:
		t.Fatalf("reset finished while the write lock was held: %v", result.err)
	default:
	}
	if err := lock.Rollback(); err != nil {
		t.Fatal(err)
	}
	successes, winningHash := 0, ""
	for range 2 {
		var outcome result
		select {
		case outcome = <-results:
		case <-ctx.Done():
			t.Fatal("reset did not finish:", ctx.Err())
		}
		if outcome.err == nil {
			successes++
			winningHash = outcome.hash
		} else {
			var missing *ErrTokenNotFound
			if !errors.As(outcome.err, &missing) {
				t.Errorf("losing redemption error = %v, want ErrTokenNotFound", outcome.err)
			}
		}
	}
	stored, err := GetUserByID(db, u.ID)
	if successes != 1 || err != nil || stored.Password != winningHash || stored.SessionVersion != u.SessionVersion+1 {
		t.Fatalf("concurrent reset: successes=%d, lookup error=%v", successes, err)
	}
}

func TestSQLiteAccountRepositoryResetRejectsInvalidInput(t *testing.T) {
	for _, scenario := range []string{"nil account", "missing ID", "nil database", "empty password", "wrong account", "missing hash", "missing expiry", "expired authorization"} {
		t.Run(scenario, func(t *testing.T) {
			db := userTestDB(t)
			repository := NewSQLiteAccountRepository(db)
			u := &User{Email: "person@example.invalid", Password: "old hash", RegistrationAuthType: "email"}
			if err := repository.Create(context.Background(), u); err != nil {
				t.Fatal(err)
			}
			u, err := repository.GetByID(context.Background(), u.ID)
			if err != nil {
				t.Fatal(err)
			}
			token, err := NewAuthResetToken(u, []byte("test-signing-key"), time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			if err := token.Create(db); err != nil {
				t.Fatal(err)
			}
			authorization := ResetAuthorization{AccountID: u.ID, TokenHash: token.TokenHash, ExpiresAt: time.Unix(token.ExpiresAt.Unix(), 0)}
			attempt := *u
			candidate, hash := &attempt, "new hash"
			switch scenario {
			case "nil account":
				candidate = nil
			case "missing ID":
				candidate.ID = ""
			case "nil database":
				repository = NewSQLiteAccountRepository(nil)
			case "empty password":
				hash = ""
			case "wrong account":
				authorization.AccountID = "another-account"
			case "missing hash":
				authorization.TokenHash = ""
			case "missing expiry":
				authorization.ExpiresAt = time.Time{}
			case "expired authorization":
				authorization.ExpiresAt = time.Now().Add(-time.Hour)
			}
			before := attempt
			if err := repository.ResetPassword(context.Background(), candidate, authorization, hash); err == nil || attempt != before {
				t.Fatalf("invalid reset succeeded or changed its account: %v", err)
			}
			stored, err := GetUserByID(db, u.ID)
			if err != nil || *stored != *u {
				t.Fatalf("invalid reset changed storage: %v", err)
			}
			if _, err := GetAuthResetTokenByHash(db, token.TokenHash); err != nil {
				t.Fatalf("invalid reset consumed the token: %v", err)
			}
		})
	}
}

type resetAccountRepository struct {
	AccountRepository
	reset func(context.Context, *User, ResetAuthorization, string) error
}

func (s resetAccountRepository) ResetPassword(ctx context.Context, u *User, authorization ResetAuthorization, hash string) error {
	return s.reset(ctx, u, authorization, hash)
}

func TestResetPasswordValidatesBeforeCallingRepository(t *testing.T) {
	for _, scenario := range []string{"success", "storage failure", "canceled", "wrong signature", "wrong purpose", "wrong account", "expired", "malformed", "empty hash", "nil account", "missing ID", "nil repository"} {
		t.Run(scenario, func(t *testing.T) {
			u := &User{ID: "account", Email: "person@example.invalid", Password: "old hash", SessionVersion: 1}
			key := []byte("test-signing-key")
			purpose, lifetime := resetTokenType, time.Hour
			if scenario == "wrong purpose" {
				purpose = verificationTokenType
			}
			token, err := newAuthToken(u, key, lifetime, purpose)
			if err != nil {
				t.Fatal(err)
			}
			hash := "new hash"
			switch scenario {
			case "wrong signature":
				key = []byte("wrong-key")
			case "wrong account":
				u.ID = "other-account"
			case "malformed":
				token.Token = "not-a-token"
			case "expired":
				claims := &VerificationClaims{Id: u.ID, Purpose: resetTokenType,
					RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour))}}
				token.Token, err = jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(key)
				if err != nil {
					t.Fatal(err)
				}
			case "empty hash":
				hash = ""
			case "nil account":
				u = nil
			case "missing ID":
				u.ID = ""
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
			var repository AccountRepository = resetAccountRepository{reset: func(got context.Context, account *User, authorization ResetAuthorization, passwordHash string) error {
				calls++
				if got != ctx || account != u || passwordHash != hash || authorization.AccountID != token.UserId || authorization.TokenHash != token.TokenHash || !authorization.ExpiresAt.Equal(time.Unix(token.ExpiresAt.Unix(), 0)) {
					t.Fatal("reset lost its context, account, password hash, or signed authorization")
				}
				return wantErr
			}}
			if scenario == "nil repository" {
				repository = nil
			}
			err = u.ResetPassword(ctx, repository, token.Token, key, hash)
			if scenario == "success" || wantErr != nil {
				if calls != 1 || !errors.Is(err, wantErr) {
					t.Fatalf("reset error=%v, repository calls=%d, want %v and 1", err, calls, wantErr)
				}
			} else if err == nil || calls != 0 {
				t.Fatalf("invalid reset reached storage: error=%v, calls=%d", err, calls)
			}
		})
	}
}

func TestResetPasswordCancellationAndExpiryDuringContention(t *testing.T) {
	for _, scenario := range []string{"cancellation", "signed expiry", "stored expiry"} {
		t.Run(scenario, func(t *testing.T) {
			db := userTestDB(t)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			u := &User{Email: "person@example.invalid", Password: "old hash", RegistrationAuthType: "email"}
			if err := u.Create(ctx, db); err != nil {
				t.Fatal(err)
			}
			key := []byte("test-signing-key")
			lifetime := time.Hour
			if scenario == "signed expiry" {
				lifetime = 2 * time.Second
			}
			token, err := NewAuthResetToken(u, key, lifetime)
			if err != nil {
				t.Fatal(err)
			}
			deadline := time.Unix(token.ExpiresAt.Unix(), 0)
			if scenario == "signed expiry" {
				// Keep stored expiry valid so only the signed deadline rejects use.
				token.ExpiresAt = time.Now().Add(time.Hour)
			} else if scenario == "stored expiry" {
				deadline = time.Now().Add(time.Second)
				token.ExpiresAt = deadline
			}
			if err := token.Create(db); err != nil {
				t.Fatal(err)
			}
			observed, started := observeTokenDeletes(t, db)
			lock, err := db.BeginTxx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Rollback()
			if _, err := lock.ExecContext(ctx, `UPDATE user SET password = password WHERE id = ?`, u.ID); err != nil {
				t.Fatal(err)
			}

			resetCtx, cancelReset := context.WithCancel(ctx)
			defer cancelReset()
			before := *u
			result := make(chan error, 1)
			go func() {
				result <- u.ResetPassword(resetCtx, NewSQLiteAccountRepository(observed), token.Token, key, "new hash")
			}()
			waitForTokenDelete(t, ctx, started)
			if scenario != "cancellation" && !time.Now().Before(deadline) {
				t.Fatal("token expired before contention was established")
			}
			if scenario == "cancellation" {
				cancelReset()
			} else {
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
			select {
			case err = <-result:
			case <-ctx.Done():
				t.Fatal("reset did not finish:", ctx.Err())
			}
			if scenario == "cancellation" {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("reset error = %v, want context.Canceled", err)
				}
			} else {
				var expired *ErrTokenExpired
				if !errors.As(err, &expired) {
					t.Fatalf("reset error = %v, want ErrTokenExpired", err)
				}
			}
			if *u != before {
				t.Error("failed reset changed the caller's account")
			}
			stored, err := GetUserByID(db, u.ID)
			if err != nil || stored.Password != before.Password {
				t.Fatalf("failed reset changed the password: %v", err)
			}
			if _, err := GetAuthResetTokenByHash(db, token.TokenHash); err != nil {
				t.Fatalf("failed reset consumed the token: %v", err)
			}
		})
	}
}

func TestResetPasswordPreservesUnrelatedAccountChanges(t *testing.T) {
	db := userTestDB(t)
	ctx := context.Background()
	u := &User{Email: "person@example.invalid", Password: "old hash", RegistrationAuthType: "email"}
	if err := u.Create(ctx, db); err != nil {
		t.Fatal(err)
	}
	key := []byte("test-signing-key")
	token, err := NewAuthResetToken(u, key, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := token.Create(db); err != nil {
		t.Fatal(err)
	}
	verified := *u
	verified.Verified = true
	if err := verified.Update(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := u.ResetPassword(ctx, NewSQLiteAccountRepository(db), token.Token, key, "new hash"); err != nil {
		t.Fatal(err)
	}
	stored, err := GetUserByID(db, u.ID)
	if err != nil || !stored.Verified || stored.Password != "new hash" {
		t.Fatalf("reset lost the independent verification change: %v", err)
	}
	if *u != *stored {
		t.Fatal("reset paired stale account fields with a current session version")
	}
	u.Email = "profile-edit@example.invalid"
	if err := u.Update(ctx, db); err != nil {
		t.Fatal(err)
	}
	stored, err = GetUserByID(db, u.ID)
	if err != nil || !stored.Verified || stored.Password != "new hash" {
		t.Fatalf("profile update after reset lost the verification change: %v", err)
	}
}
