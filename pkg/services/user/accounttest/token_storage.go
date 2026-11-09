package accounttest

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

// RunTokens checks token storage for the supplied purposes. Run calls it for
// advertised reset, verification, password-setup, and invitation capabilities.
// Token-only adapters can call it directly with their supported purposes.
// Each factory call must provide isolated storage and register cleanup with t.
func RunTokens(t *testing.T, open func(*testing.T) user.AccountRepository, purposes ...string) {
	t.Helper()
	for _, purpose := range purposes {
		switch purpose {
		case user.ResetTokenPurpose, user.VerificationTokenPurpose, user.PasswordSetupTokenPurpose, user.InvitationTokenPurpose:
		default:
			t.Fatalf("unknown token purpose %q", purpose)
		}
		t.Run("token storage/"+purpose, func(t *testing.T) {
			t.Run("metadata and insert only", func(t *testing.T) {
				repo := open(t)
				storage := capability[user.TokenRepository](t, repo)
				u := create(t, repo, "token@example.invalid")
				token := insertToken(t, repo, u.ID, purpose)
				other := token
				other.TokenHash = tokenHash("other token")
				must(t, storage.InsertToken(t.Context(), other))
				assertToken(t, repo, token)
				assertToken(t, repo, token) // Reads must not consume the token.
				duplicate := token
				duplicate.ExpiresAt = token.ExpiresAt.Add(time.Hour)
				if len(purposes) > 1 {
					duplicate.Purpose = purposes[(slices.Index(purposes, purpose)+1)%len(purposes)]
				}
				if err := storage.InsertToken(t.Context(), duplicate); err == nil {
					t.Fatal("duplicate insert replaced token metadata")
				}
				assertToken(t, repo, token)
				assertToken(t, repo, other)
				wrongPurpose := token
				wrongPurpose.Purpose = user.ResetTokenPurpose
				if purpose == wrongPurpose.Purpose {
					wrongPurpose.Purpose = user.VerificationTokenPurpose
				}
				assertTokenMissing(t, repo, wrongPurpose)
				missing := token
				missing.TokenHash = tokenHash("missing")
				assertTokenMissing(t, repo, missing)
				assertAccount(t, load(t, repo, u.ID), u)
			})

			t.Run("concurrent insert", func(t *testing.T) {
				repo := open(t)
				storage := capability[user.TokenRepository](t, repo)
				u := create(t, repo, "token@example.invalid")
				first := user.TokenMetadata{AccountID: u.ID, TokenHash: tokenHash("duplicate"), Purpose: purpose, ExpiresAt: tokenExpiry(time.Hour), Email: u.Email}
				second := first
				second.ExpiresAt = first.ExpiresAt.Add(time.Hour)
				if len(purposes) > 1 {
					second.Purpose = purposes[(slices.Index(purposes, purpose)+1)%len(purposes)]
				}
				tokens := [2]user.TokenMetadata{first, second}
				winner := oneSuccess(t, concurrent(func(i int) error {
					return storage.InsertToken(t.Context(), tokens[i])
				}))
				assertToken(t, repo, tokens[winner])
				if first.Purpose != second.Purpose {
					assertTokenMissing(t, repo, tokens[1-winner])
				}
				assertAccount(t, load(t, repo, u.ID), u)
			})

			for _, operation := range []string{"lookup", "insert"} {
				t.Run("cancellation/"+operation, func(t *testing.T) {
					repo := open(t)
					storage := capability[user.TokenRepository](t, repo)
					u := create(t, repo, "cancel@example.invalid")
					token := insertToken(t, repo, u.ID, purpose)
					newToken := token
					newToken.TokenHash = tokenHash("canceled insert")
					ctx, cancel := context.WithCancel(t.Context())
					cancel()
					var metadata user.TokenMetadata
					var err error
					if operation == "lookup" {
						metadata, err = storage.GetToken(ctx, token.TokenHash, purpose)
					} else {
						err = storage.InsertToken(ctx, newToken)
					}
					if !errors.Is(err, context.Canceled) || metadata != (user.TokenMetadata{}) {
						t.Fatalf("canceled token operation = %+v, %v", metadata, err)
					}
					assertToken(t, repo, token)
					assertTokenMissing(t, repo, newToken)
					assertAccount(t, load(t, repo, u.ID), u)
				})
			}
			accountTokenUpdates(t, open, purpose)
		})
	}
}

func accountTokenUpdates(t *testing.T, open func(*testing.T) user.AccountRepository, purpose string) {
	for _, change := range []string{"no change", "email spelling", "email key", "password", "provider", "verified", "verification required", "role", "invited", "creation time", "duplicate email", "stale version", "missing account", "canceled update", "password change", "canceled password change"} {
		t.Run("account update/"+change, func(t *testing.T) {
			repo := open(t)
			if strings.Contains(change, "password change") {
				capability[user.PasswordRepository](t, repo)
			}
			original := create(t, repo, "É@bücher.invalid")
			other := create(t, repo, "other@example.invalid")
			token := insertToken(t, repo, original.ID, purpose)
			second := token
			second.TokenHash = tokenHash("second token")
			must(t, capability[user.TokenRepository](t, repo).InsertToken(t.Context(), second))
			otherToken := insertToken(t, repo, other.ID, purpose)
			changed := original
			ctx := t.Context()
			var wantError error
			switch change {
			case "email spelling":
				changed.Email = "é@xn--bcher-kva.invalid"
			case "email key":
				changed.Email = "changed@example.invalid"
			case "password":
				changed.Password = "new hash"
			case "provider":
				changed.RegistrationAuthType = "another provider"
			case "verified":
				changed.Verified = true
			case "verification required":
				changed.VerificationRequired = false
			case "role":
				changed.Role = "poster"
			case "invited":
				changed.Invited = true
			case "creation time":
				changed.CreatedAt = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
			case "duplicate email":
				changed.Email, wantError = strings.ToUpper(other.Email), user.ErrEmailAlreadyExists
			case "stale version":
				changed.Email, wantError = "changed@example.invalid", user.ErrUserChanged
				changed.SessionVersion--
			case "missing account":
				changed.ID, wantError = "missing", user.ErrUserNotFound
			case "canceled update", "canceled password change":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				wantError = context.Canceled
				if change == "canceled update" {
					changed.Email = "changed@example.invalid"
				}
			}
			var err error
			if strings.Contains(change, "password change") {
				err = capability[user.PasswordRepository](t, repo).ChangePassword(ctx, &changed, "new hash")
			} else {
				err = repo.Update(ctx, &changed)
			}
			if !errors.Is(err, wantError) {
				t.Fatalf("update error = %v, want %v", err, wantError)
			}
			if change == "email key" && (purpose == user.VerificationTokenPurpose || purpose == user.InvitationTokenPurpose) {
				assertTokenMissing(t, repo, token)
				assertTokenMissing(t, repo, second)
			} else {
				assertToken(t, repo, token)
				assertToken(t, repo, second)
			}
			assertToken(t, repo, otherToken)
			assertAccount(t, load(t, repo, other.ID), other)
		})
	}
}

func tokenPurposes(repo user.AccountRepository) []string {
	var purposes []string
	if _, ok := repo.(user.PasswordResetRepository); ok {
		purposes = append(purposes, user.ResetTokenPurpose)
	}
	if _, ok := repo.(user.VerificationRepository); ok {
		purposes = append(purposes, user.VerificationTokenPurpose)
	}
	if _, ok := repo.(user.PasswordSetupRepository); ok {
		purposes = append(purposes, user.PasswordSetupTokenPurpose)
	}
	if _, ok := repo.(user.InvitationRepository); ok {
		purposes = append(purposes, user.InvitationTokenPurpose)
	}
	return purposes
}

func optionalToken(t *testing.T, repo user.AccountRepository, id, purpose string) user.TokenMetadata {
	t.Helper()
	if !slices.Contains(tokenPurposes(repo), purpose) {
		return user.TokenMetadata{}
	}
	return insertToken(t, repo, id, purpose)
}

func requireRedemption(t *testing.T, repo user.AccountRepository, purpose string) {
	t.Helper()
	if purpose == user.VerificationTokenPurpose {
		capability[user.VerificationRepository](t, repo)
	} else {
		capability[user.PasswordResetRepository](t, repo)
	}
}
