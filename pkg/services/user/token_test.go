package user

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestTokenPurposeStorage(t *testing.T) {
	for _, purpose := range []string{resetTokenType, verificationTokenType} {
		t.Run(purpose, func(t *testing.T) {
			db := userTestDB(t)
			u := &User{Email: "person@example.invalid"}
			if err := u.Create(context.Background(), db); err != nil {
				t.Fatal(err)
			}
			key := []byte("test-signing-key")
			var token AuthToken
			if purpose == resetTokenType {
				reset, err := NewAuthResetToken(u, key, time.Hour)
				if err != nil {
					t.Fatal(err)
				}
				token = reset.AuthToken
			} else {
				verification, err := NewAuthVerificationToken(u, key, time.Hour)
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
			if purpose == verificationTokenType && claims["email"] != u.Email {
				t.Error("verification token is not bound to the recipient address")
			}
			if err := token.Create(db); err != nil {
				t.Fatal(err)
			}
			byHash, err := getTokenByHash(db, token.TokenHash, purpose)
			if err != nil || byHash.Type != purpose {
				t.Fatalf("stored token lookup by hash failed: %v", err)
			}
			byUser, err := getTokenByUser(db, u.ID, purpose)
			if err != nil || byUser.Type != purpose {
				t.Fatalf("stored token lookup by user failed: %v", err)
			}
		})
	}
}

func TestTokensCannotSubstituteForEachOther(t *testing.T) {
	db := userTestDB(t)
	u := &User{Email: "person@example.invalid", Password: "unchanged", VerificationRequired: true}
	if err := u.Create(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	key := []byte("test-signing-key")
	reset, err := NewAuthResetToken(u, key, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := reset.Create(db); err != nil {
		t.Fatal(err)
	}
	verification, err := NewAuthVerificationToken(u, key, 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := verification.Create(db); err != nil {
		t.Fatal(err)
	}
	if got, err := Verify(context.Background(), NewSQLiteAccountRepository(db), reset.Token, key); err == nil || got != nil {
		t.Error("reset token verified the account")
	}
	if got, err := ValidateResetToken(db, verification.Token, key); err == nil || got != nil {
		t.Error("verification token authorized a password reset")
	}
	if got, err := ValidateVerificationToken(db, reset.Token, key); err == nil || got != nil {
		t.Error("reset token passed stored verification-token validation")
	}
	stored, err := GetUserByID(db, u.ID)
	if err != nil || stored.Verified || stored.Password != u.Password {
		t.Fatal("wrong-purpose token changed the account")
	}
	if got, err := ValidateResetToken(db, reset.Token, key); err != nil || got.ID != u.ID {
		t.Fatalf("valid reset token rejected: %v", err)
	}
	if got, err := ValidateVerificationToken(db, verification.Token, key); err != nil || got.ID != u.ID {
		t.Fatalf("valid stored verification token rejected: %v", err)
	}
	if got, err := Verify(context.Background(), NewSQLiteAccountRepository(db), verification.Token, key); err != nil || !got.Verified {
		t.Fatalf("valid verification token rejected: %v", err)
	}
}

func TestSignedTokenValidation(t *testing.T) {
	for _, purpose := range []string{resetTokenType, verificationTokenType} {
		for _, failure := range []string{
			"missing purpose", "unknown purpose", "wrong purpose", "missing user",
			"missing expiry", "null expiry", "zero expiry", "invalid expiry", "expired", "expired wrong key",
			"wrong key", "empty key", "HS384", "HS512", "unsigned",
			"future issued at", "future not before", "malformed",
		} {
			t.Run(purpose+"/"+failure, func(t *testing.T) {
				claims := jwt.MapClaims{"Id": "test-user", "purpose": purpose, "email": "person@example.invalid", "exp": time.Now().Add(time.Hour).Unix()}
				key := []byte("test-signing-key")
				var signingKey any = key
				var method jwt.SigningMethod = jwt.SigningMethodHS256
				switch failure {
				case "missing purpose":
					delete(claims, "purpose")
				case "unknown purpose":
					claims["purpose"] = "session"
				case "wrong purpose":
					claims["purpose"] = resetTokenType
					if purpose == resetTokenType {
						claims["purpose"] = verificationTokenType
					}
				case "missing user":
					delete(claims, "Id")
				case "missing expiry":
					delete(claims, "exp")
				case "null expiry":
					claims["exp"] = nil
				case "zero expiry":
					claims["exp"] = 0
				case "invalid expiry":
					claims["exp"] = "tomorrow"
				case "expired", "expired wrong key":
					claims["exp"] = time.Now().Add(-time.Hour).Unix()
				case "HS384":
					method = jwt.SigningMethodHS384
				case "HS512":
					method = jwt.SigningMethodHS512
				case "unsigned":
					method, signingKey = jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType
				case "future issued at":
					claims["iat"] = time.Now().Add(time.Hour).Unix()
				case "future not before":
					claims["nbf"] = time.Now().Add(time.Hour).Unix()
				}
				raw, err := jwt.NewWithClaims(method, claims).SignedString(signingKey)
				if err != nil {
					t.Fatal(err)
				}
				switch failure {
				case "wrong key", "expired wrong key":
					key = []byte("another-key")
				case "empty key":
					key = nil
				case "malformed":
					raw = "not-a-token"
				}
				if _, err := parseAuthToken(raw, key, purpose); err == nil {
					t.Fatal("invalid token accepted")
				} else if failure == "expired" {
					var expired *ErrTokenExpired
					if !errors.As(err, &expired) {
						t.Fatalf("error = %v, want ErrTokenExpired", err)
					}
				} else if failure == "expired wrong key" {
					var expired *ErrTokenExpired
					if errors.As(err, &expired) {
						t.Fatal("invalid signature was classified as an expired valid token")
					}
				}
			})
		}
	}
}

func TestVerifyRejectsUnboundOrExpiredSignedTokens(t *testing.T) {
	for _, failure := range []string{"missing email", "signed expiry"} {
		t.Run(failure, func(t *testing.T) {
			db, u, _, key := verificationFixture(t)
			claims := VerificationClaims{Id: u.ID, Purpose: verificationTokenType, Email: u.Email,
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
			token := AuthToken{UserId: u.ID, TokenHash: hex.EncodeToString(hash[:]), Type: verificationTokenType, ExpiresAt: time.Now().Add(time.Hour)}
			if err := token.Create(db); err != nil {
				t.Fatal(err)
			}
			if got, err := Verify(context.Background(), NewSQLiteAccountRepository(db), raw, key); err == nil || got != nil {
				t.Fatal("invalid signed verification token accepted")
			}
			stored, err := GetUserByID(db, u.ID)
			if err != nil || stored.Verified {
				t.Fatalf("invalid token verified account: %v", err)
			}
			if _, err := GetAuthVerificationTokenByHash(db, token.TokenHash); err != nil {
				t.Fatalf("invalid token was consumed: %v", err)
			}
		})
	}
}

func TestCreateTokenHonorsCancellation(t *testing.T) {
	db, u, _, key := verificationFixture(t)
	token, err := NewAuthVerificationToken(u, key, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := token.CreateContext(ctx, db); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled creation returned %v", err)
	}
	var missing *ErrTokenNotFound
	if _, err := GetAuthVerificationTokenByHash(db, token.TokenHash); !errors.As(err, &missing) {
		t.Fatalf("canceled creation stored token: %v", err)
	}
}

func TestLegacyStoredResetTokenRejected(t *testing.T) {
	db := userTestDB(t)
	u := &User{Email: "person@example.invalid"}
	if err := u.Create(context.Background(), db); err != nil {
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
	token := AuthToken{UserId: u.ID, TokenHash: hex.EncodeToString(hash[:]), ExpiresAt: expires, Type: resetTokenType}
	if err := token.Create(db); err != nil {
		t.Fatal(err)
	}
	if got, err := ValidateResetToken(db, raw, key); err == nil || got != nil {
		t.Fatal("stored legacy token without a purpose was accepted")
	}
	if got, err := Verify(context.Background(), NewSQLiteAccountRepository(db), raw, key); err == nil || got != nil {
		t.Fatal("legacy token without a purpose verified the account")
	}
}

func TestTokenRejectsUnsupportedPurpose(t *testing.T) {
	for _, purpose := range []string{"", "session"} {
		t.Run(purpose, func(t *testing.T) {
			db := userTestDB(t)
			u := &User{Email: "person@example.invalid"}
			if err := u.Create(context.Background(), db); err != nil {
				t.Fatal(err)
			}
			if _, err := newAuthToken(&User{ID: "test-user"}, []byte("test-key"), time.Hour, purpose); err == nil {
				t.Fatal("created a token with an unsupported purpose")
			}
			token := AuthToken{UserId: u.ID, TokenHash: "test-hash", ExpiresAt: time.Now().Add(time.Hour), Type: purpose}
			var invalid *ErrToken
			if err := token.Create(db); !errors.As(err, &invalid) || invalid.Unwrap() == nil || invalid.Unwrap().Error() != "invalid token purpose" {
				t.Fatalf("unsupported purpose error = %v, want invalid token purpose", err)
			}
			var count int
			if err := db.Get(&count, `SELECT COUNT(*) FROM usertoken`); err != nil || count != 0 {
				t.Fatalf("unsupported purpose inserted a token: count=%d, error=%v", count, err)
			}
		})
	}
}
