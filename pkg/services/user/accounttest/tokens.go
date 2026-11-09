package accounttest

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

func tokenContract(t *testing.T, open func(*testing.T) user.AccountRepository) {
	for _, pair := range [][2]string{
		{"É@bücher.invalid", "é@xn--bcher-kva.invalid"},
		{"用户@例え.invalid", "用户@xn--r8jz45g.invalid"},
		{"Person@bu\u0308cher.invalid", "person@bücher.invalid"},
	} {
		t.Run("verification after equivalent email update/"+pair[0], func(t *testing.T) {
			repo := open(t)
			capability[user.VerificationRepository](t, repo)
			original := create(t, repo, pair[0])
			token := insertToken(t, repo, original.ID, user.VerificationTokenPurpose)
			changed := original
			changed.Email = pair[1]
			must(t, repo.Update(t.Context(), &changed))
			assertToken(t, repo, token)
			// Authorization with the original spelling must still identify the account.
			verified, err := redeem(t, t.Context(), repo, &original, token)
			must(t, err)
			if verified == nil || !verified.Verified || verified.Email != pair[1] {
				t.Fatalf("verification after equivalent update = %+v", verified)
			}
		})
	}
	for _, verified := range []bool{false, true} {
		t.Run(fmt.Sprintf("verification accepts invited account/%t", verified), func(t *testing.T) {
			repo := open(t)
			capability[user.VerificationRepository](t, repo)
			u := newAccount("invited@example.invalid")
			u.Role, u.Invited, u.Verified = "poster", true, verified
			must(t, repo.Create(t.Context(), &u))
			token := insertToken(t, repo, u.ID, user.VerificationTokenPurpose)
			reset := optionalToken(t, repo, u.ID, user.ResetTokenPurpose)
			got, err := redeem(t, t.Context(), repo, &u, token)
			must(t, err)
			if got == nil {
				t.Fatal("verification returned nil")
			}
			want := u
			want.Invited, want.Verified = false, true
			want.SessionVersion++
			want.UpdatedAt = got.UpdatedAt
			assertAccount(t, *got, want)
			assertAccount(t, load(t, repo, u.ID), want)
			assertTokenMissing(t, repo, token)
			assertToken(t, repo, reset)
		})
	}
	for _, operation := range []string{"email change", "verification", "failed email change", "failed verification"} {
		t.Run("invitation token revocation/"+operation, func(t *testing.T) {
			repo := open(t)
			if _, ok := repo.(user.InvitationRepository); !ok {
				t.Skip("provider does not support invitations")
			}
			if strings.Contains(operation, "verification") {
				capability[user.VerificationRepository](t, repo)
			}
			u := newAccount("invited@example.invalid")
			u.Role, u.Invited = "poster", true
			must(t, repo.Create(t.Context(), &u))
			invitation := insertToken(t, repo, u.ID, user.InvitationTokenPurpose)
			verification := optionalToken(t, repo, u.ID, user.VerificationTokenPurpose)
			reset := optionalToken(t, repo, u.ID, user.ResetTokenPurpose)
			other := create(t, repo, "other@example.invalid")
			otherInvite := insertToken(t, repo, other.ID, user.InvitationTokenPurpose)
			changed := u
			var err error
			switch operation {
			case "email change", "failed email change":
				changed.Email = "changed@example.invalid"
				if operation == "failed email change" {
					changed.SessionVersion--
				}
				err = repo.Update(t.Context(), &changed)
			default:
				if operation == "failed verification" {
					changed.Email = "wrong@example.invalid"
				}
				_, err = redeem(t, t.Context(), repo, &changed, verification)
			}
			if strings.HasPrefix(operation, "failed") {
				if err == nil {
					t.Fatal("invalid account change succeeded")
				}
				assertAccount(t, load(t, repo, u.ID), u)
				assertToken(t, repo, invitation)
				assertToken(t, repo, verification)
			} else {
				must(t, err)
				assertTokenMissing(t, repo, invitation)
				assertTokenMissing(t, repo, verification)
			}
			assertToken(t, repo, reset)
			assertToken(t, repo, otherInvite)
		})
	}
	t.Run("verification recipient binding", func(t *testing.T) {
		repo := open(t)
		capability[user.VerificationRepository](t, repo)
		u := create(t, repo, "current@example.invalid")
		token := user.TokenMetadata{AccountID: u.ID, TokenHash: tokenHash("old recipient"), Purpose: user.VerificationTokenPurpose, ExpiresAt: tokenExpiry(time.Hour), Email: "old@example.invalid"}
		must(t, capability[user.TokenRepository](t, repo).InsertToken(t.Context(), token))
		for _, email := range []string{u.Email, token.Email} {
			got, err := capability[user.VerificationRepository](t, repo).Verify(t.Context(), user.VerificationAuthorization{AccountID: u.ID, TokenHash: token.TokenHash, ExpiresAt: token.ExpiresAt, Email: email})
			if got != nil || !errors.Is(err, user.ErrToken) {
				t.Fatalf("mismatched recipient accepted: %v, %v", got, err)
			}
			assertToken(t, repo, token)
			assertAccount(t, load(t, repo, u.ID), u)
		}
	})

	for _, purpose := range []string{user.ResetTokenPurpose, user.VerificationTokenPurpose} {
		t.Run(purpose+"/redeem and replay", func(t *testing.T) {
			repo := open(t)
			requireRedemption(t, repo, purpose)
			original := create(t, repo, "MixedΣ@example.invalid")
			token := insertToken(t, repo, original.ID, purpose)
			unrelated := token
			unrelated.TokenHash = tokenHash("another token")
			must(t, capability[user.TokenRepository](t, repo).InsertToken(t.Context(), unrelated))
			other := create(t, repo, "other@example.invalid")
			otherToken := insertToken(t, repo, other.ID, purpose)
			input := original
			input.Email = "MIXEDς@EXAMPLE.INVALID"
			got, err := redeem(t, t.Context(), repo, &input, token)
			must(t, err)
			if got == nil {
				t.Fatal("redemption returned nil account")
			}
			assertTimestamp(t, got.UpdatedAt)
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
			got, err = redeem(t, t.Context(), repo, &input, token)
			if !errors.Is(err, user.ErrTokenNotFound) || got != nil {
				t.Fatalf("replay = %v, %v", got, err)
			}
			assertAccount(t, input, beforeReplay)
			assertAccount(t, load(t, repo, original.ID), want)
			assertToken(t, repo, unrelated)
			assertToken(t, repo, otherToken)
			assertAccount(t, load(t, repo, other.ID), other)
		})

		for _, failure := range []string{"stored expiry", "supplied expiry", "wrong purpose", "wrong hash", "wrong account", "account mismatch"} {
			t.Run(purpose+"/rollback/"+failure, func(t *testing.T) {
				repo := open(t)
				requireRedemption(t, repo, purpose)
				original := create(t, repo, "rollback@example.invalid")
				other := create(t, repo, "other@example.invalid")
				token := user.TokenMetadata{AccountID: original.ID, TokenHash: tokenHash("rollback"), Purpose: purpose, ExpiresAt: tokenExpiry(time.Hour), Email: original.Email}
				if failure == "stored expiry" {
					token.ExpiresAt = tokenExpiry(-time.Hour)
				}
				if failure == "wrong purpose" {
					token.Purpose = user.ResetTokenPurpose
					if purpose == user.ResetTokenPurpose {
						token.Purpose = user.VerificationTokenPurpose
					}
					requireRedemption(t, repo, token.Purpose)
				}
				must(t, capability[user.TokenRepository](t, repo).InsertToken(t.Context(), token))
				// Preliminary lookup must expose expired metadata without consuming it.
				assertToken(t, repo, token)
				auth := token
				auth.Purpose, auth.ExpiresAt = purpose, tokenExpiry(time.Hour)
				input := original
				switch failure {
				case "supplied expiry":
					auth.ExpiresAt = tokenExpiry(-time.Hour)
				case "wrong hash":
					auth.TokenHash = tokenHash("missing")
				case "wrong account":
					auth.AccountID = other.ID
					input = other
				case "account mismatch":
					input.Password, input.Email = "stale password", "stale@example.invalid"
				}
				before := input
				got, err := redeem(t, t.Context(), repo, &input, auth)
				if err == nil || got != nil {
					t.Fatalf("invalid redemption = %v, %v", got, err)
				}
				if strings.Contains(failure, "expiry") {
					if !errors.Is(err, user.ErrTokenExpired) {
						t.Fatalf("expiry error = %v", err)
					}
				} else if failure != "account mismatch" {
					if !errors.Is(err, user.ErrTokenNotFound) {
						t.Fatalf("token mismatch error = %v", err)
					}
				}
				assertAccount(t, input, before)
				assertAccount(t, load(t, repo, original.ID), original)
				assertAccount(t, load(t, repo, other.ID), other)
				assertToken(t, repo, token)
				if failure != "stored expiry" {
					_, err := redeem(t, t.Context(), repo, &original, token)
					must(t, err)
					assertTokenMissing(t, repo, token)
				}
			})
		}

		t.Run(purpose+"/concurrent redemption", func(t *testing.T) {
			repo := open(t)
			requireRedemption(t, repo, purpose)
			original := create(t, repo, "concurrent@example.invalid")
			token := insertToken(t, repo, original.ID, purpose)
			inputs := [2]user.User{original, original}
			var results [2]*user.User
			errs := concurrent(func(i int) error {
				var err error
				results[i], err = redeem(t, t.Context(), repo, &inputs[i], token)
				return err
			})
			winner := oneSuccess(t, errs)
			if results[winner] == nil || results[1-winner] != nil || !errors.Is(errs[1-winner], user.ErrTokenNotFound) {
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
		capability[user.PasswordResetRepository](t, repo)
		input := create(t, repo, "provider@example.invalid")
		before := input
		token := insertToken(t, repo, input.ID, user.ResetTokenPurpose)
		current := input
		current.RegistrationAuthType = "another provider"
		must(t, repo.Update(t.Context(), &current))
		got, err := redeem(t, t.Context(), repo, &input, token)
		if err == nil || got != nil {
			t.Fatalf("reset with stale provider = %v, %v", got, err)
		}
		assertAccount(t, input, before)
		assertAccount(t, load(t, repo, input.ID), current)
		assertToken(t, repo, token)
	})

	t.Run("reset refreshes unrelated account changes", func(t *testing.T) {
		repo := open(t)
		capability[user.PasswordResetRepository](t, repo)
		input := create(t, repo, "reset@example.invalid")
		token := insertToken(t, repo, input.ID, user.ResetTokenPurpose)
		current := input
		current.Email, current.Verified, current.VerificationRequired = "updated@example.invalid", true, false
		must(t, repo.Update(t.Context(), &current))
		got, err := redeem(t, t.Context(), repo, &input, token)
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
		capability[user.VerificationRepository](t, repo)
		u := create(t, repo, "verified@example.invalid")
		u.Verified = true
		must(t, repo.Update(t.Context(), &u))
		token := insertToken(t, repo, u.ID, user.VerificationTokenPurpose)
		got, err := redeem(t, t.Context(), repo, &u, token)
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
	for _, operation := range []string{"lookup ID", "lookup email", "create", "update", "change password", "reset", "verify"} {
		t.Run("cancellation/"+operation, func(t *testing.T) {
			repo := open(t)
			original := create(t, repo, "cancel@example.invalid")
			var token user.TokenMetadata
			if operation == "reset" || operation == "verify" {
				purpose := user.ResetTokenPurpose
				if operation == "verify" {
					purpose = user.VerificationTokenPurpose
				}
				requireRedemption(t, repo, purpose)
				token = insertToken(t, repo, original.ID, purpose)
			}
			input := original
			if operation == "create" {
				input = newAccount("new@example.invalid")
			} else if operation == "update" {
				input.Email = "updated@example.invalid"
			}
			before := input
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			var result *user.User
			var err error
			switch operation {
			case "lookup ID":
				result, err = repo.GetByID(ctx, input.ID)
			case "lookup email":
				result, err = repo.GetByEmail(ctx, input.Email)
			case "create":
				err = repo.Create(ctx, &input)
			case "update":
				err = repo.Update(ctx, &input)
			case "change password":
				err = capability[user.PasswordRepository](t, repo).ChangePassword(ctx, &input, "new hash")
			case "reset", "verify":
				result, err = redeem(t, ctx, repo, &input, token)
			}
			if !errors.Is(err, context.Canceled) || result != nil {
				t.Fatalf("canceled operation = %v, %v", result, err)
			}
			assertAccount(t, input, before)
			assertAccount(t, load(t, repo, original.ID), original)
			assertToken(t, repo, token)
			if operation == "create" || operation == "update" {
				got, err := repo.GetByEmail(t.Context(), input.Email)
				if got != nil || !errors.Is(err, user.ErrUserNotFound) {
					t.Fatalf("canceled write persisted email: %v, %v", got, err)
				}
			}
		})
	}
}

func tokenHash(value string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(value))) }

// Use whole seconds away from the boundary. Storage-specific tests check finer
// precision and exact deadlines using the provider's clock.
func tokenExpiry(offset time.Duration) time.Time {
	return time.Now().UTC().Truncate(time.Second).Add(offset)
}

func insertToken(t *testing.T, repo user.AccountRepository, id, purpose string) user.TokenMetadata {
	t.Helper()
	token := user.TokenMetadata{AccountID: id, TokenHash: tokenHash(id + purpose), Purpose: purpose, ExpiresAt: tokenExpiry(time.Hour)}
	token.Email = load(t, repo, id).Email
	must(t, capability[user.TokenRepository](t, repo).InsertToken(t.Context(), token))
	return token
}

func assertToken(t *testing.T, repo user.AccountRepository, want user.TokenMetadata) {
	t.Helper()
	if want.AccountID == "" {
		return
	}
	got, err := capability[user.TokenRepository](t, repo).GetToken(t.Context(), want.TokenHash, want.Purpose)
	must(t, err)
	got.ExpiresAt, want.ExpiresAt = got.ExpiresAt.Round(0).UTC(), want.ExpiresAt.Round(0).UTC()
	if got != want {
		t.Fatalf("token = %+v, want %+v", got, want)
	}
}

func assertTokenMissing(t *testing.T, repo user.AccountRepository, token user.TokenMetadata) {
	t.Helper()
	if token.AccountID == "" {
		return
	}
	got, err := capability[user.TokenRepository](t, repo).GetToken(t.Context(), token.TokenHash, token.Purpose)
	if !errors.Is(err, user.ErrTokenNotFound) || got != (user.TokenMetadata{}) {
		t.Fatalf("missing token = %+v, %v", got, err)
	}
}

func redeem(t *testing.T, ctx context.Context, repo user.AccountRepository, input *user.User, token user.TokenMetadata) (*user.User, error) {
	if token.Purpose == user.VerificationTokenPurpose {
		return capability[user.VerificationRepository](t, repo).Verify(ctx, user.VerificationAuthorization{
			AccountID: token.AccountID, TokenHash: token.TokenHash, Email: input.Email, ExpiresAt: token.ExpiresAt,
		})
	}
	if err := capability[user.PasswordResetRepository](t, repo).ResetPassword(ctx, input, user.ResetAuthorization{
		AccountID: token.AccountID, TokenHash: token.TokenHash, ExpiresAt: token.ExpiresAt,
	}, "reset password hash"); err != nil {
		return nil, err
	}
	return input, nil
}
