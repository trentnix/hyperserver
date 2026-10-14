package accounttest

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

func tokenContract(t *testing.T, open func(*testing.T) user.AccountRepository) {
	t.Run("token metadata and insert only", func(t *testing.T) {
		repo := open(t)
		u := create(t, repo, "token@example.invalid")
		token := insertToken(t, repo, u.ID, user.ResetTokenPurpose)
		other := insertToken(t, repo, u.ID, user.VerificationTokenPurpose)
		assertToken(t, repo, token)
		assertToken(t, repo, token) // Lookup must not consume the token.
		duplicate := token
		duplicate.Purpose = other.Purpose
		duplicate.ExpiresAt = token.ExpiresAt.Add(time.Hour)
		if err := repo.InsertToken(t.Context(), duplicate); err == nil {
			t.Fatal("duplicate insert replaced token metadata")
		}
		assertToken(t, repo, token)
		assertToken(t, repo, other)
		wrongPurpose := token
		wrongPurpose.Purpose = other.Purpose
		assertTokenMissing(t, repo, wrongPurpose)
		missing := token
		missing.TokenHash = tokenHash("missing")
		assertTokenMissing(t, repo, missing)
		assertAccount(t, load(t, repo, u.ID), u)
	})

	t.Run("concurrent token insert", func(t *testing.T) {
		repo := open(t)
		u := create(t, repo, "token@example.invalid")
		first := user.TokenMetadata{AccountID: u.ID, TokenHash: tokenHash("duplicate"), Purpose: user.ResetTokenPurpose, ExpiresAt: time.Now().Add(time.Hour)}
		second := first
		second.Purpose = user.VerificationTokenPurpose
		tokens := [2]user.TokenMetadata{first, second}
		winner := oneSuccess(t, concurrent(func(i int) error { return repo.InsertToken(t.Context(), tokens[i]) }))
		assertToken(t, repo, tokens[winner])
		assertTokenMissing(t, repo, tokens[1-winner])
		assertAccount(t, load(t, repo, u.ID), u)
	})

	for _, purpose := range []string{user.ResetTokenPurpose, user.VerificationTokenPurpose} {
		for _, expiry := range []string{"stored", "signed"} {
			t.Run(purpose+"/expiry boundary/"+expiry, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					repo := open(t)
					original := create(t, repo, "expiry@example.invalid")
					token := insertToken(t, repo, original.ID, purpose)
					auth := token
					deadline := token.ExpiresAt
					if expiry == "stored" {
						auth.ExpiresAt = deadline.Add(time.Hour)
					} else {
						deadline = deadline.Add(-time.Minute)
						auth.ExpiresAt = deadline
					}
					// Advance the virtual clock to the exact deadline, without sleeping in real time.
					assertToken(t, repo, token)
					time.Sleep(time.Until(deadline))
					input := original
					got, err := redeem(t.Context(), repo, &input, auth)
					var expired *user.ErrTokenExpired
					if got != nil || !errors.As(err, &expired) {
						t.Fatalf("redemption at expiry = %v, %v", got, err)
					}
					assertAccount(t, input, original)
					assertAccount(t, load(t, repo, original.ID), original)
					assertToken(t, repo, token)
				})
			})
		}

		t.Run(purpose+"/redeem and replay", func(t *testing.T) {
			repo := open(t)
			original := create(t, repo, "MixedΣ@example.invalid")
			token := insertToken(t, repo, original.ID, purpose)
			unrelated := token
			unrelated.TokenHash = tokenHash("another token")
			must(t, repo.InsertToken(t.Context(), unrelated))
			other := create(t, repo, "other@example.invalid")
			otherToken := insertToken(t, repo, other.ID, purpose)
			input := original
			input.Email = "MIXEDς@EXAMPLE.INVALID"
			started := time.Now()
			got, err := redeem(t.Context(), repo, &input, token)
			must(t, err)
			if got == nil {
				t.Fatal("redemption returned nil account")
			}
			assertUpdateTime(t, got.UpdatedAt, started)
			want := original
			want.UpdatedAt, want.SessionVersion = got.UpdatedAt, original.SessionVersion+1
			if purpose == user.ResetTokenPurpose {
				want.Password = "reset password hash"
				assertAccount(t, input, want)
			} else {
				want.Verified = true
			}
			assertAccount(t, *got, want)
			assertAccount(t, load(t, repo, original.ID), want)
			assertTokenMissing(t, repo, token)

			beforeReplay := input
			got, err = redeem(t.Context(), repo, &input, token)
			var missing *user.ErrTokenNotFound
			if !errors.As(err, &missing) || got != nil {
				t.Fatalf("replay = %v, %v", got, err)
			}
			assertAccount(t, input, beforeReplay)
			assertAccount(t, load(t, repo, original.ID), want)
			assertToken(t, repo, unrelated)
			assertToken(t, repo, otherToken)
			assertAccount(t, load(t, repo, other.ID), other)
		})

		for _, failure := range []string{"stored expiry", "signed expiry", "wrong purpose", "wrong hash", "wrong account", "account mismatch"} {
			t.Run(purpose+"/rollback/"+failure, func(t *testing.T) {
				repo := open(t)
				original := create(t, repo, "rollback@example.invalid")
				other := create(t, repo, "other@example.invalid")
				token := user.TokenMetadata{AccountID: original.ID, TokenHash: tokenHash("rollback"), Purpose: purpose, ExpiresAt: time.Now().Add(time.Hour)}
				if failure == "stored expiry" {
					token.ExpiresAt = time.Now().Add(-time.Second)
				}
				if failure == "wrong purpose" {
					token.Purpose = user.ResetTokenPurpose
					if purpose == user.ResetTokenPurpose {
						token.Purpose = user.VerificationTokenPurpose
					}
				}
				must(t, repo.InsertToken(t.Context(), token))
				// Preliminary lookup must expose expired metadata without consuming it.
				assertToken(t, repo, token)
				auth := token
				auth.Purpose, auth.ExpiresAt = purpose, time.Now().Add(time.Hour)
				input := original
				switch failure {
				case "signed expiry":
					auth.ExpiresAt = time.Now().Add(-time.Second)
				case "wrong hash":
					auth.TokenHash = tokenHash("missing")
				case "wrong account":
					auth.AccountID = other.ID
					input = other
				case "account mismatch":
					input.Password, input.Email = "stale password", "stale@example.invalid"
				}
				before := input
				got, err := redeem(t.Context(), repo, &input, auth)
				if err == nil || got != nil {
					t.Fatalf("invalid redemption = %v, %v", got, err)
				}
				if strings.Contains(failure, "expiry") {
					var expired *user.ErrTokenExpired
					if !errors.As(err, &expired) {
						t.Fatalf("expiry error = %v", err)
					}
				} else if failure != "account mismatch" {
					var missing *user.ErrTokenNotFound
					if !errors.As(err, &missing) {
						t.Fatalf("token mismatch error = %v", err)
					}
				}
				assertAccount(t, input, before)
				assertAccount(t, load(t, repo, original.ID), original)
				assertAccount(t, load(t, repo, other.ID), other)
				assertToken(t, repo, token)
				if failure != "stored expiry" {
					_, err := redeem(t.Context(), repo, &original, token)
					must(t, err)
					assertTokenMissing(t, repo, token)
				}
			})
		}

		t.Run(purpose+"/concurrent redemption", func(t *testing.T) {
			repo := open(t)
			original := create(t, repo, "concurrent@example.invalid")
			token := insertToken(t, repo, original.ID, purpose)
			inputs := [2]user.User{original, original}
			var results [2]*user.User
			errs := concurrent(func(i int) error {
				var err error
				results[i], err = redeem(t.Context(), repo, &inputs[i], token)
				return err
			})
			winner := oneSuccess(t, errs)
			var missing *user.ErrTokenNotFound
			if results[winner] == nil || results[1-winner] != nil || !errors.As(errs[1-winner], &missing) {
				t.Fatalf("concurrent redemption results = %v, %v", results, errs)
			}
			want := original
			want.SessionVersion++
			want.UpdatedAt = results[winner].UpdatedAt
			if purpose == user.ResetTokenPurpose {
				want.Password = "reset password hash"
			} else {
				want.Verified = true
			}
			assertAccount(t, *results[winner], want)
			assertAccount(t, inputs[1-winner], original)
			assertAccount(t, load(t, repo, original.ID), want)
			assertTokenMissing(t, repo, token)
		})
	}

	t.Run("reset rejects changed provider", func(t *testing.T) {
		repo := open(t)
		input := create(t, repo, "provider@example.invalid")
		before := input
		token := insertToken(t, repo, input.ID, user.ResetTokenPurpose)
		current := input
		current.RegistrationAuthType = "another provider"
		must(t, repo.Update(t.Context(), &current))
		got, err := redeem(t.Context(), repo, &input, token)
		if err == nil || got != nil {
			t.Fatalf("reset with stale provider = %v, %v", got, err)
		}
		assertAccount(t, input, before)
		assertAccount(t, load(t, repo, input.ID), current)
		assertToken(t, repo, token)
	})

	t.Run("reset refreshes unrelated account changes", func(t *testing.T) {
		repo := open(t)
		input := create(t, repo, "reset@example.invalid")
		token := insertToken(t, repo, input.ID, user.ResetTokenPurpose)
		current := input
		current.Email, current.Verified, current.VerificationRequired = "updated@example.invalid", true, false
		must(t, repo.Update(t.Context(), &current))
		got, err := redeem(t.Context(), repo, &input, token)
		must(t, err)
		if got == nil {
			t.Fatal("reset returned nil")
		}
		want := current
		want.Password, want.UpdatedAt = "reset password hash", got.UpdatedAt
		want.SessionVersion++
		assertAccount(t, input, want)
		assertAccount(t, load(t, repo, input.ID), want)
	})

	t.Run("verified account keeps security version", func(t *testing.T) {
		repo := open(t)
		u := create(t, repo, "verified@example.invalid")
		u.Verified = true
		must(t, repo.Update(t.Context(), &u))
		token := insertToken(t, repo, u.ID, user.VerificationTokenPurpose)
		got, err := redeem(t.Context(), repo, &u, token)
		must(t, err)
		if got == nil {
			t.Fatal("verification returned nil")
		}
		want := u
		want.UpdatedAt = got.UpdatedAt
		assertAccount(t, *got, want)
		assertAccount(t, load(t, repo, u.ID), want)
		assertTokenMissing(t, repo, token)
	})
}

func cancellationContract(t *testing.T, open func(*testing.T) user.AccountRepository) {
	for _, operation := range []string{"lookup ID", "lookup email", "lookup token", "create", "update", "insert token", "change password", "reset", "verify"} {
		t.Run("cancellation/"+operation, func(t *testing.T) {
			repo := open(t)
			original := create(t, repo, "cancel@example.invalid")
			reset := insertToken(t, repo, original.ID, user.ResetTokenPurpose)
			verification := insertToken(t, repo, original.ID, user.VerificationTokenPurpose)
			input := original
			if operation == "create" {
				input = newAccount("new@example.invalid")
			} else if operation == "update" {
				input.Email = "updated@example.invalid"
			}
			before := input
			newToken := reset
			newToken.TokenHash = tokenHash("canceled insert")
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			var result *user.User
			var metadata user.TokenMetadata
			var err error
			switch operation {
			case "lookup ID":
				result, err = repo.GetByID(ctx, input.ID)
			case "lookup email":
				result, err = repo.GetByEmail(ctx, input.Email)
			case "lookup token":
				metadata, err = repo.GetToken(ctx, reset.TokenHash, reset.Purpose)
			case "create":
				err = repo.Create(ctx, &input)
			case "update":
				err = repo.Update(ctx, &input)
			case "insert token":
				err = repo.InsertToken(ctx, newToken)
			case "change password":
				err = repo.ChangePassword(ctx, &input, "new hash")
			case "reset":
				result, err = redeem(ctx, repo, &input, reset)
			case "verify":
				result, err = redeem(ctx, repo, &input, verification)
			}
			if !errors.Is(err, context.Canceled) || result != nil || metadata != (user.TokenMetadata{}) {
				t.Fatalf("canceled operation = %v, %v, %v", result, metadata, err)
			}
			assertAccount(t, input, before)
			assertAccount(t, load(t, repo, original.ID), original)
			assertToken(t, repo, reset)
			assertToken(t, repo, verification)
			assertTokenMissing(t, repo, newToken)
			if operation == "create" || operation == "update" {
				got, err := repo.GetByEmail(t.Context(), input.Email)
				var missing *user.ErrUserNotFound
				if got != nil || !errors.As(err, &missing) {
					t.Fatalf("canceled write persisted email: %v, %v", got, err)
				}
			}
		})
	}
}

func tokenHash(value string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(value))) }

func insertToken(t *testing.T, repo user.AccountRepository, id, purpose string) user.TokenMetadata {
	t.Helper()
	token := user.TokenMetadata{AccountID: id, TokenHash: tokenHash(id + purpose), Purpose: purpose, ExpiresAt: time.Now().Add(time.Hour)}
	must(t, repo.InsertToken(t.Context(), token))
	return token
}

func assertToken(t *testing.T, repo user.AccountRepository, want user.TokenMetadata) {
	t.Helper()
	got, err := repo.GetToken(t.Context(), want.TokenHash, want.Purpose)
	must(t, err)
	got.ExpiresAt, want.ExpiresAt = got.ExpiresAt.Round(0).UTC(), want.ExpiresAt.Round(0).UTC()
	if got != want {
		t.Fatalf("token = %+v, want %+v", got, want)
	}
}

func assertTokenMissing(t *testing.T, repo user.AccountRepository, token user.TokenMetadata) {
	t.Helper()
	got, err := repo.GetToken(t.Context(), token.TokenHash, token.Purpose)
	var missing *user.ErrTokenNotFound
	if !errors.As(err, &missing) || got != (user.TokenMetadata{}) {
		t.Fatalf("missing token = %+v, %v", got, err)
	}
}

func redeem(ctx context.Context, repo user.AccountRepository, input *user.User, token user.TokenMetadata) (*user.User, error) {
	if token.Purpose == user.VerificationTokenPurpose {
		return repo.Verify(ctx, user.VerificationAuthorization{
			AccountID: token.AccountID, TokenHash: token.TokenHash, Email: input.Email, ExpiresAt: token.ExpiresAt,
		})
	}
	if err := repo.ResetPassword(ctx, input, user.ResetAuthorization{
		AccountID: token.AccountID, TokenHash: token.TokenHash, ExpiresAt: token.ExpiresAt,
	}, "reset password hash"); err != nil {
		return nil, err
	}
	return input, nil
}
