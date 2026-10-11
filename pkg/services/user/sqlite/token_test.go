package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/golang-jwt/jwt/v5"
	accounts "github.com/trentnix/hyperserver/pkg/services/user"
)

func TestTokenPurposeStorage(t *testing.T) {
	for _, purpose := range []string{accounts.ResetTokenPurpose, accounts.VerificationTokenPurpose} {
		t.Run(purpose, func(t *testing.T) {
			db := userTestDB(t)
			u := &accounts.User{Email: "person@example.invalid"}
			if err := NewAccountRepository(db).Create(context.Background(), u); err != nil {
				t.Fatal(err)
			}
			key := []byte("test-signing-key")
			var token accounts.AuthToken
			if purpose == accounts.ResetTokenPurpose {
				reset, err := accounts.NewAuthResetToken(u, key, time.Hour)
				if err != nil {
					t.Fatal(err)
				}
				token = reset.AuthToken
			} else {
				verification, err := accounts.NewAuthVerificationToken(u, key, time.Hour)
				if err != nil {
					t.Fatal(err)
				}
				token = verification.AuthToken
			}
			if token.Type != purpose {
				t.Errorf("token type = %q, want %q", token.Type, purpose)
			}
			claims := jwt.MapClaims{}
			if _, err := jwt.ParseWithClaims(token.Token, claims, func(*jwt.Token) (interface{}, error) { return key, nil }); err != nil {
				t.Fatal(err)
			}
			if claims["purpose"] != purpose {
				t.Errorf("signed purpose = %v, want %q", claims["purpose"], purpose)
			}
			if purpose == accounts.VerificationTokenPurpose && claims["email"] != u.Email {
				t.Error("verification token is not bound to the recipient address")
			}
			repository := NewAccountRepository(db)
			if err := repository.InsertToken(context.Background(), token.Metadata()); err != nil {
				t.Fatal(err)
			}
			var persisted accounts.TokenMetadata
			err := db.QueryRowContext(t.Context(), `SELECT user_id, token_hash, expires_at, token_type FROM usertoken`).
				Scan(&persisted.AccountID, &persisted.TokenHash, &persisted.ExpiresAt, &persisted.Purpose)
			hash := sha256.Sum256([]byte(token.Token))
			if err != nil || persisted.AccountID != u.ID || persisted.TokenHash != hex.EncodeToString(hash[:]) || persisted.Purpose != purpose || !persisted.ExpiresAt.Equal(token.ExpiresAt) {
				t.Fatalf("persisted token metadata = %+v, %v", persisted, err)
			}

			byHash, err := NewAccountRepository(db).GetToken(t.Context(), token.TokenHash, purpose)
			if err != nil || byHash.Purpose != purpose || byHash.AccountID != u.ID || byHash.TokenHash != token.TokenHash || !byHash.ExpiresAt.Equal(token.ExpiresAt) {
				t.Fatalf("stored token lookup by hash failed: %v", err)
			}
			duplicate := token.Metadata()
			duplicate.ExpiresAt = duplicate.ExpiresAt.Add(time.Hour)
			duplicate.Purpose = "auth-reset"
			if purpose == accounts.ResetTokenPurpose {
				duplicate.Purpose = accounts.VerificationTokenPurpose
			}
			if err := repository.InsertToken(context.Background(), duplicate); err == nil {
				t.Fatal("duplicate insertion replaced the stored token")
			}
			stored, err := NewAccountRepository(db).GetToken(t.Context(), token.TokenHash, purpose)
			if err != nil || stored != byHash {
				t.Fatalf("duplicate insertion changed token metadata: %v", err)
			}
			resend, err := newTestToken(u, key, time.Hour, purpose)
			if err != nil {
				t.Fatal(err)
			}
			if err := repository.InsertToken(context.Background(), resend.Metadata()); err != nil {
				t.Fatal(err)
			}
			for _, raw := range []string{token.Token, resend.Token} {
				validate := accounts.ValidateResetToken
				if purpose == accounts.VerificationTokenPurpose {
					validate = accounts.ValidateVerificationToken
				}
				if _, err := validate(context.Background(), NewAccountRepository(db), raw, key); err != nil {
					t.Fatalf("resend invalidated an issued token: %v", err)
				}
			}
		})
	}
}

func TestTokensCannotSubstituteForEachOther(t *testing.T) {
	db := userTestDB(t)
	u := &accounts.User{Email: "person@example.invalid", Password: "unchanged", VerificationRequired: true}
	if err := NewAccountRepository(db).Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	key := []byte("test-signing-key")
	reset, err := accounts.NewAuthResetToken(u, key, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewAccountRepository(db).InsertToken(t.Context(), reset.Metadata()); err != nil {
		t.Fatal(err)
	}
	verification, err := accounts.NewAuthVerificationToken(u, key, 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewAccountRepository(db).InsertToken(t.Context(), verification.Metadata()); err != nil {
		t.Fatal(err)
	}
	if got, err := accounts.Verify(context.Background(), NewAccountRepository(db), reset.Token, key); err == nil || got != nil {
		t.Error("reset token verified the account")
	}
	if got, err := accounts.ValidateResetToken(context.Background(), NewAccountRepository(db), verification.Token, key); err == nil || got != nil {
		t.Error("verification token authorized a password reset")
	}
	if got, err := accounts.ValidateVerificationToken(context.Background(), NewAccountRepository(db), reset.Token, key); err == nil || got != nil {
		t.Error("reset token passed stored verification-token validation")
	}
	stored, err := NewAccountRepository(db).GetByID(t.Context(), u.ID)
	if err != nil || stored.Verified || stored.Password != u.Password {
		t.Fatal("wrong-purpose token changed the account")
	}
	if got, err := accounts.ValidateResetToken(context.Background(), NewAccountRepository(db), reset.Token, key); err != nil || got.ID != u.ID {
		t.Fatalf("valid reset token rejected: %v", err)
	}
	if got, err := accounts.ValidateVerificationToken(context.Background(), NewAccountRepository(db), verification.Token, key); err != nil || got.ID != u.ID {
		t.Fatalf("valid stored verification token rejected: %v", err)
	}
	if got, err := accounts.Verify(context.Background(), NewAccountRepository(db), verification.Token, key); err != nil || !got.Verified {
		t.Fatalf("valid verification token rejected: %v", err)
	}
}

func TestVerifyRejectsUnboundOrExpiredSignedTokens(t *testing.T) {
	for _, failure := range []string{"missing email", "signed expiry"} {
		t.Run(failure, func(t *testing.T) {
			db, u, _, key := verificationFixture(t)
			claims := accounts.VerificationClaims{Id: u.ID, Purpose: accounts.VerificationTokenPurpose, Email: u.Email,
				RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}}
			if failure == "missing email" {
				claims.Email = ""
			} else {
				claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Hour))
			}
			raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(key)
			if err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256([]byte(raw))
			token := accounts.AuthToken{UserId: u.ID, TokenHash: hex.EncodeToString(hash[:]), Type: accounts.VerificationTokenPurpose, ExpiresAt: time.Now().Add(time.Hour)}
			if err := NewAccountRepository(db).InsertToken(t.Context(), token.Metadata()); err != nil {
				t.Fatal(err)
			}
			if got, err := accounts.Verify(context.Background(), NewAccountRepository(db), raw, key); err == nil || got != nil {
				t.Fatal("invalid signed verification token accepted")
			}
			stored, err := NewAccountRepository(db).GetByID(t.Context(), u.ID)
			if err != nil || stored.Verified {
				t.Fatalf("invalid token verified account: %v", err)
			}
			if _, err := NewAccountRepository(db).GetToken(t.Context(), token.TokenHash, accounts.VerificationTokenPurpose); err != nil {
				t.Fatalf("invalid token was consumed: %v", err)
			}
		})
	}
}

func TestInsertTokenHonorsCancellation(t *testing.T) {
	db, u, _, key := verificationFixture(t)
	token, err := accounts.NewAuthVerificationToken(u, key, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := NewAccountRepository(db).InsertToken(ctx, token.Metadata()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled creation returned %v", err)
	}
	var missing *accounts.ErrTokenNotFound
	if _, err := NewAccountRepository(db).GetToken(t.Context(), token.TokenHash, accounts.VerificationTokenPurpose); !errors.As(err, &missing) {
		t.Fatalf("canceled creation stored token: %v", err)
	}
}

func TestSQLiteAccountRepositoryInsertTokenFailure(t *testing.T) {
	for _, scenario := range []string{"nil database", "closed database", "insert failure", "missing account", "missing hash", "missing expiry", "invalid purpose"} {
		t.Run(scenario, func(t *testing.T) {
			db, u, original, _ := verificationFixture(t)
			repository := NewAccountRepository(db)
			metadata := original.Metadata()
			metadata.TokenHash = "new hash"
			switch scenario {
			case "nil database":
				repository = NewAccountRepository(nil)
			case "closed database":
				closed := userTestDB(t)
				if err := closed.Close(); err != nil {
					t.Fatal(err)
				}
				repository = NewAccountRepository(closed)
			case "insert failure":
				if _, err := db.Exec(`CREATE TRIGGER reject_token BEFORE INSERT ON usertoken BEGIN SELECT RAISE(ABORT, 'write failed'); END`); err != nil {
					t.Fatal(err)
				}
			case "missing account":
				metadata.AccountID = ""
			case "missing hash":
				metadata.TokenHash = ""
			case "missing expiry":
				metadata.ExpiresAt = time.Time{}
			case "invalid purpose":
				metadata.Purpose = "other"
			}
			if err := repository.InsertToken(context.Background(), metadata); err == nil {
				t.Fatal("failed token insertion returned success")
			}
			var count int
			if err := db.Get(&count, `SELECT count(*) FROM usertoken`); err != nil || count != 1 {
				t.Fatalf("failed insertion changed stored tokens: count=%d, error=%v", count, err)
			}
			stored, err := NewAccountRepository(db).GetToken(t.Context(), original.TokenHash, accounts.VerificationTokenPurpose)
			if err != nil || stored.AccountID != u.ID || !stored.ExpiresAt.Equal(original.ExpiresAt) {
				t.Fatalf("failed insertion changed the original token: %v", err)
			}
		})
	}
}

func TestSQLiteAccountRepositoryInsertTokenCancellationWhileWaiting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		db, _, token, _ := verificationFixture(t)
		metadata := token.Metadata()
		metadata.TokenHash = "new hash"
		connection, err := db.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer connection.Close()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		result := make(chan error, 1)
		go func() { result <- NewAccountRepository(db).InsertToken(ctx, metadata) }()
		synctest.Wait()
		select {
		case err := <-result:
			t.Fatalf("insertion returned before a connection was available: %v", err)
		default:
		}

		cancel()
		if err := <-result; !errors.Is(err, context.Canceled) {
			t.Fatalf("insertion error=%v, want context.Canceled", err)
		}
		if err := connection.Close(); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := db.Get(&count, `SELECT count(*) FROM usertoken`); err != nil || count != 1 {
			t.Fatalf("canceled insertion changed stored tokens: count=%d, error=%v", count, err)
		}
	})
}

func TestSQLiteAccountRepositoryConcurrentTokenInsertion(t *testing.T) {
	db, _, token, _ := verificationFixture(t)
	repository := NewAccountRepository(db)
	metadata := token.Metadata()
	metadata.TokenHash = "new hash"
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			results <- repository.InsertToken(context.Background(), metadata)
		}()
	}
	close(start)
	successes := 0
	for range 2 {
		if err := <-results; err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful duplicate insertions=%d, want 1", successes)
	}
	stored, err := NewAccountRepository(db).GetToken(t.Context(), metadata.TokenHash, metadata.Purpose)
	if err != nil || stored.AccountID != metadata.AccountID || !stored.ExpiresAt.Equal(metadata.ExpiresAt) {
		t.Fatalf("concurrent insertion lost token metadata: %v", err)
	}
	var count int
	if err := db.Get(&count, `SELECT count(*) FROM usertoken`); err != nil || count != 2 {
		t.Fatalf("concurrent insertion changed other tokens: count=%d, error=%v", count, err)
	}
}

func TestSQLiteAccountRepositoryGetToken(t *testing.T) {
	for _, purpose := range []string{accounts.ResetTokenPurpose, accounts.VerificationTokenPurpose} {
		t.Run(purpose, func(t *testing.T) {
			db, u, _, key := verificationFixture(t)
			repository := NewAccountRepository(db)
			token, err := newTestToken(u, key, time.Hour, purpose)
			if err != nil {
				t.Fatal(err)
			}
			if err := repository.InsertToken(context.Background(), token.Metadata()); err != nil {
				t.Fatal(err)
			}
			for _, expired := range []bool{false, true} {
				if expired {
					token.ExpiresAt = time.Now().Add(-time.Hour)
					if _, err := db.Exec(`UPDATE usertoken SET expires_at = ? WHERE token_hash = ?`, token.ExpiresAt, token.TokenHash); err != nil {
						t.Fatal(err)
					}
				}
				for range 2 {
					got, err := repository.GetToken(context.Background(), token.TokenHash, purpose)
					if err != nil || got.AccountID != u.ID || got.TokenHash != token.TokenHash || got.Purpose != purpose || !got.ExpiresAt.Equal(token.ExpiresAt) {
						t.Fatalf("lookup lost metadata or consumed token: %+v, %v", got, err)
					}
				}
			}
		})
	}
}

func TestSQLiteAccountRepositoryGetTokenFailures(t *testing.T) {
	for _, scenario := range []string{"missing", "wrong purpose", "empty hash", "invalid purpose", "nil database", "closed database", "missing schema", "invalid stored expiry", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			db, _, token, _ := verificationFixture(t)
			repository := NewAccountRepository(db)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			hash, purpose := token.TokenHash, accounts.VerificationTokenPurpose
			switch scenario {
			case "missing":
				hash = "missing"
			case "wrong purpose":
				purpose = accounts.ResetTokenPurpose
			case "empty hash":
				hash = ""
			case "invalid purpose":
				purpose = "unknown"
			case "nil database":
				repository = NewAccountRepository(nil)
			case "closed database":
				closed := userTestDB(t)
				if err := closed.Close(); err != nil {
					t.Fatal(err)
				}
				repository = NewAccountRepository(closed)
			case "missing schema":
				repository = NewAccountRepository(unpreparedUserTestDB(t))
			case "invalid stored expiry":
				if _, err := db.ExecContext(ctx, `UPDATE usertoken SET expires_at = CAST('invalid' AS BLOB) WHERE token_hash = ?`, hash); err != nil {
					t.Fatal(err)
				}
			case "canceled":
				cancel()
			}
			got, err := repository.GetToken(ctx, hash, purpose)
			var missing *accounts.ErrTokenNotFound
			wantMissing := scenario == "missing" || scenario == "wrong purpose"
			if err == nil || got != (accounts.TokenMetadata{}) || errors.As(err, &missing) != wantMissing {
				t.Fatalf("failed lookup returned metadata or wrong error: %+v, %v", got, err)
			}
			if scenario == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled lookup error=%v", err)
			}
		})
	}
}

func TestSQLiteAccountRepositoryGetTokenCancellationWhileWaiting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		db, _, token, _ := verificationFixture(t)
		connection, err := db.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer connection.Close()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		result := make(chan error, 1)
		go func() {
			_, err := NewAccountRepository(db).GetToken(ctx, token.TokenHash, token.Type)
			result <- err
		}()
		synctest.Wait()
		select {
		case err := <-result:
			t.Fatalf("lookup did not wait for a connection: %v", err)
		default:
		}
		cancel()
		if err := <-result; !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled lookup error=%v", err)
		}
		if err := connection.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := NewAccountRepository(db).GetToken(t.Context(), token.TokenHash, accounts.VerificationTokenPurpose); err != nil {
			t.Fatalf("canceled lookup consumed token: %v", err)
		}
	})
}

func TestLegacyStoredResetTokenRejected(t *testing.T) {
	db := userTestDB(t)
	u := &accounts.User{Email: "person@example.invalid"}
	if err := NewAccountRepository(db).Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	key := []byte("test-signing-key")
	expires := time.Now().Add(time.Hour)
	claims := jwt.MapClaims{"Id": u.ID, "exp": expires.Unix()}
	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(raw))
	token := accounts.AuthToken{UserId: u.ID, TokenHash: hex.EncodeToString(hash[:]), ExpiresAt: expires, Type: accounts.ResetTokenPurpose}
	if err := NewAccountRepository(db).InsertToken(t.Context(), token.Metadata()); err != nil {
		t.Fatal(err)
	}
	if got, err := accounts.ValidateResetToken(context.Background(), NewAccountRepository(db), raw, key); err == nil || got != nil {
		t.Fatal("stored legacy token without a purpose was accepted")
	}
	if got, err := accounts.Verify(context.Background(), NewAccountRepository(db), raw, key); err == nil || got != nil {
		t.Fatal("legacy token without a purpose verified the account")
	}
}

func TestTokenRejectsUnsupportedPurpose(t *testing.T) {
	for _, purpose := range []string{"", "session"} {
		t.Run(purpose, func(t *testing.T) {
			db := userTestDB(t)
			u := &accounts.User{Email: "person@example.invalid"}
			if err := NewAccountRepository(db).Create(context.Background(), u); err != nil {
				t.Fatal(err)
			}
			token := accounts.AuthToken{UserId: u.ID, TokenHash: "test-hash", ExpiresAt: time.Now().Add(time.Hour), Type: purpose}
			var invalid *accounts.ErrToken
			if err := NewAccountRepository(db).InsertToken(t.Context(), token.Metadata()); !errors.As(err, &invalid) || invalid.Unwrap() == nil || invalid.Unwrap().Error() != "invalid token purpose" {
				t.Fatalf("unsupported purpose error = %v, want invalid token purpose", err)
			}
			var count int
			if err := db.Get(&count, `SELECT COUNT(*) FROM usertoken`); err != nil || count != 0 {
				t.Fatalf("unsupported purpose inserted a token: count=%d, error=%v", count, err)
			}
		})
	}
}
