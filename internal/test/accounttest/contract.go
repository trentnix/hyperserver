// Package accounttest checks account storage through its consumer contract.
// It is test support, not an application storage implementation.
package accounttest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

// Run checks an initialized repository. Each factory call must provide isolated
// storage and register cleanup with t. Backend-specific I/O failures, locking,
// schema mappings, clock boundaries, timestamp precision, and durability tests
// remain with the provider. Returned timestamps must match committed values.
func Run(t *testing.T, open func(*testing.T) user.AccountRepository) {
	t.Helper()
	t.Run("create and field preservation", func(t *testing.T) {
		repo := open(t)
		for _, lookup := range []func(context.Context, string) (*user.User, error){repo.GetByID, repo.GetByEmail} {
			got, err := lookup(t.Context(), "missing@example.invalid")
			var missing *user.ErrUserNotFound
			if got != nil || !errors.As(err, &missing) {
				t.Fatalf("missing account = %v, %v", got, err)
			}
		}

		original := newAccount("O'Neil.Σ@example.invalid")
		want := original
		must(t, repo.Create(t.Context(), &original))
		if original.ID == "" || original.SessionVersion != 1 || original.CreatedAt.IsZero() || original.UpdatedAt.IsZero() {
			t.Fatal("create did not supply identity, timestamps, and initial security version")
		}
		want.ID, want.CreatedAt, want.UpdatedAt, want.SessionVersion = original.ID, original.CreatedAt, original.UpdatedAt, 1
		assertAccount(t, original, want)
		byEmail, err := repo.GetByEmail(t.Context(), "o'neil.ς@EXAMPLE.INVALID")
		must(t, err)
		if byEmail == nil {
			t.Fatal("successful lookup returned nil")
		}
		assertAccount(t, *byEmail, original)
		original.Email = "not saved"
		assertAccount(t, load(t, repo, want.ID), want)

		// Exercise both boolean values and a different provider at creation.
		verified := newAccount("verified@example.invalid")
		verified.Verified, verified.VerificationRequired = true, false
		verified.RegistrationAuthType = "another provider"
		must(t, repo.Create(t.Context(), &verified))
		assertAccount(t, load(t, repo, verified.ID), verified)
		if !verified.Verified || verified.VerificationRequired || verified.RegistrationAuthType != "another provider" {
			t.Fatal("create changed supplied account fields")
		}
	})

	for _, by := range []string{"ID", "email"} {
		t.Run("independent reads/"+by, func(t *testing.T) {
			repo := open(t)
			original := create(t, repo, "independent@example.invalid")
			lookup, key := repo.GetByID, original.ID
			if by == "email" {
				lookup, key = repo.GetByEmail, original.Email
			}
			first, err := lookup(t.Context(), key)
			must(t, err)
			second, err := lookup(t.Context(), key)
			must(t, err)
			if first == nil || second == nil || first == second {
				t.Fatal("reads must return independent, non-nil account pointers")
			}
			assertAccount(t, *first, original)
			assertAccount(t, *second, original)

			first.Email, first.Password = "not-saved@example.invalid", "not saved"
			assertAccount(t, *second, original)
			third, err := lookup(t.Context(), key)
			must(t, err)
			if third == nil {
				t.Fatal("successful lookup returned nil")
			}
			assertAccount(t, *third, original)
			assertAccount(t, load(t, repo, original.ID), original)
		})
	}

	t.Run("duplicate creation is insert only", func(t *testing.T) {
		repo := open(t)
		original := create(t, repo, "MixedΣ@example.invalid")
		duplicate := newAccount("MIXEDς@EXAMPLE.INVALID")
		before := duplicate
		if err := repo.Create(t.Context(), &duplicate); !errors.Is(err, user.ErrEmailAlreadyExists) {
			t.Fatalf("duplicate error = %v", err)
		}
		assertAccount(t, duplicate, before)
		assertAccount(t, load(t, repo, original.ID), original)
	})

	for _, field := range []string{"no change", "email case", "email key", "password", "provider", "verified", "verification required", "creation time"} {
		t.Run("update/"+field, func(t *testing.T) {
			repo := open(t)
			original := create(t, repo, "Mixed@example.invalid")
			verification := insertToken(t, repo, original.ID, user.VerificationTokenPurpose)
			secondVerification := verification
			secondVerification.TokenHash = tokenHash("second verification")
			must(t, repo.InsertToken(t.Context(), secondVerification))
			reset := insertToken(t, repo, original.ID, user.ResetTokenPurpose)
			other := create(t, repo, "other@example.invalid")
			otherToken := insertToken(t, repo, other.ID, user.VerificationTokenPurpose)
			changed := original
			increment := true
			switch field {
			case "no change":
				increment = false
			case "email case":
				changed.Email = strings.ToUpper(changed.Email)
				increment = false
			case "email key":
				changed.Email = "different@example.invalid"
			case "password":
				changed.Password = "new hash"
			case "provider":
				changed.RegistrationAuthType = "another provider"
			case "verified":
				changed.Verified = true
			case "verification required":
				changed.VerificationRequired = false
			case "creation time":
				changed.CreatedAt = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
				increment = false
			}
			want := changed
			want.CreatedAt = original.CreatedAt
			if increment {
				want.SessionVersion++
			}
			must(t, repo.Update(t.Context(), &changed))
			assertTimestamp(t, changed.UpdatedAt)
			want.UpdatedAt = changed.UpdatedAt
			if changed.SessionVersion != want.SessionVersion {
				t.Fatalf("caller version = %d, want %d", changed.SessionVersion, want.SessionVersion)
			}
			assertAccount(t, load(t, repo, original.ID), want)
			if field == "email key" {
				assertTokenMissing(t, repo, verification)
				assertTokenMissing(t, repo, secondVerification)
				got, err := repo.GetByEmail(t.Context(), original.Email)
				var missing *user.ErrUserNotFound
				if got != nil || !errors.As(err, &missing) {
					t.Fatalf("old email still resolves: %v, %v", got, err)
				}
			} else {
				assertToken(t, repo, verification)
				assertToken(t, repo, secondVerification)
			}
			got, err := repo.GetByEmail(t.Context(), strings.ToUpper(want.Email))
			must(t, err)
			if got == nil {
				t.Fatal("updated email lookup returned nil")
			}
			assertAccount(t, *got, want)
			assertToken(t, repo, reset)
			assertToken(t, repo, otherToken)
			assertAccount(t, load(t, repo, other.ID), other)
		})
	}

	for _, failure := range []string{"duplicate email", "stale version", "missing account"} {
		t.Run("update rollback/"+failure, func(t *testing.T) {
			repo := open(t)
			original := create(t, repo, "original@example.invalid")
			other := create(t, repo, "other@example.invalid")
			token := insertToken(t, repo, original.ID, user.VerificationTokenPurpose)
			changed := original
			changed.Email = "different@example.invalid"
			switch failure {
			case "duplicate email":
				changed.Email = strings.ToUpper(other.Email)
			case "stale version":
				changed.SessionVersion--
			case "missing account":
				changed.ID = "missing"
			}
			before := changed
			err := repo.Update(t.Context(), &changed)
			var stale *user.ErrUserChanged
			var missing *user.ErrUserNotFound
			if (failure == "duplicate email" && !errors.Is(err, user.ErrEmailAlreadyExists)) ||
				(failure == "stale version" && !errors.As(err, &stale)) ||
				(failure == "missing account" && !errors.As(err, &missing)) {
				t.Fatalf("update error = %v", err)
			}
			assertAccount(t, changed, before)
			assertAccount(t, load(t, repo, original.ID), original)
			assertAccount(t, load(t, repo, other.ID), other)
			assertToken(t, repo, token)
		})
	}

	for _, operation := range []string{"create", "update", "change password"} {
		t.Run("concurrent writes/"+operation, func(t *testing.T) {
			repo := open(t)
			first := newAccount("Mixed@example.invalid")
			if operation != "create" {
				must(t, repo.Create(t.Context(), &first))
			}
			second := first
			if operation == "create" {
				second.Email = strings.ToUpper(second.Email)
			}
			inputs := [2]user.User{first, second}
			errs := concurrent(func(i int) error {
				switch operation {
				case "create":
					return repo.Create(t.Context(), &inputs[i])
				case "update":
					inputs[i].Password = "new hash"
					return repo.Update(t.Context(), &inputs[i])
				default:
					return repo.ChangePassword(t.Context(), &inputs[i], "new hash")
				}
			})
			winner := oneSuccess(t, errs)
			assertAccount(t, load(t, repo, inputs[winner].ID), inputs[winner])
			loser := 1 - winner
			wantLoser := [2]user.User{first, second}[loser]
			if operation == "update" {
				wantLoser.Password = "new hash"
				var stale *user.ErrUserChanged
				if !errors.As(errs[loser], &stale) {
					t.Fatalf("concurrent update error = %v", errs[loser])
				}
			}
			if operation == "create" && !errors.Is(errs[loser], user.ErrEmailAlreadyExists) {
				t.Fatalf("concurrent create error = %v", errs[loser])
			}
			assertAccount(t, inputs[loser], wantLoser)
		})
	}

	t.Run("concurrent independent accounts", func(t *testing.T) {
		repo := open(t)
		inputs := [2]user.User{newAccount("first@example.invalid"), newAccount("second@example.invalid")}
		errs := concurrent(func(i int) error { return repo.Create(t.Context(), &inputs[i]) })
		for i, err := range errs {
			must(t, err)
			assertAccount(t, load(t, repo, inputs[i].ID), inputs[i])
		}
		if inputs[0].ID == inputs[1].ID {
			t.Fatal("independent accounts received the same ID")
		}
	})

	passwordContract(t, open)
	tokenContract(t, open)
	cancellationContract(t, open)
}

func passwordContract(t *testing.T, open func(*testing.T) user.AccountRepository) {
	for _, mismatch := range []string{"none", "password", "provider", "version", "missing account"} {
		t.Run("password change/"+mismatch, func(t *testing.T) {
			repo := open(t)
			original := create(t, repo, "password@example.invalid")
			token := insertToken(t, repo, original.ID, user.ResetTokenPurpose)
			changed := original
			switch mismatch {
			case "password":
				changed.Password = "stale hash"
			case "provider":
				changed.RegistrationAuthType = "different provider"
			case "version":
				changed.SessionVersion--
			case "missing account":
				changed.ID = "missing"
			}
			before, want := changed, original
			err := repo.ChangePassword(t.Context(), &changed, "new hash")
			if mismatch == "none" {
				must(t, err)
				assertTimestamp(t, changed.UpdatedAt)
				want.Password, want.UpdatedAt = "new hash", changed.UpdatedAt
				want.SessionVersion++
				assertAccount(t, changed, want)
			} else {
				if err == nil {
					t.Fatal("password change accepted a mismatched account")
				}
				assertAccount(t, changed, before)
			}
			assertAccount(t, load(t, repo, original.ID), want)
			assertToken(t, repo, token)
		})
	}
}

func newAccount(email string) user.User {
	return user.User{Email: email, Password: "stored password hash", RegistrationAuthType: "email", VerificationRequired: true}
}

func create(t *testing.T, repo user.AccountRepository, email string) user.User {
	t.Helper()
	u := newAccount(email)
	must(t, repo.Create(t.Context(), &u))
	return u
}

func load(t *testing.T, repo user.AccountRepository, id string) user.User {
	t.Helper()
	u, err := repo.GetByID(t.Context(), id)
	must(t, err)
	if u == nil {
		t.Fatal("successful lookup returned nil")
	}
	return *u
}

func assertAccount(t *testing.T, got, want user.User) {
	t.Helper()
	// Storage need not preserve a time's location or monotonic clock reading.
	got.CreatedAt, want.CreatedAt = got.CreatedAt.Round(0).UTC(), want.CreatedAt.Round(0).UTC()
	got.UpdatedAt, want.UpdatedAt = got.UpdatedAt.Round(0).UTC(), want.UpdatedAt.Round(0).UTC()
	if got != want {
		t.Fatalf("account = %+v, want %+v", got, want)
	}
}

func assertTimestamp(t *testing.T, got time.Time) {
	t.Helper()
	if got.IsZero() {
		t.Fatal("operation did not supply a timestamp")
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// Collect both results before asserting, so cleanup cannot race a losing writer.
func concurrent(operation func(int) error) [2]error {
	start := make(chan struct{})
	results := [2]chan error{make(chan error, 1), make(chan error, 1)}
	for i := range results {
		go func() {
			<-start
			results[i] <- operation(i)
		}()
	}
	close(start)
	return [2]error{<-results[0], <-results[1]}
}

func oneSuccess(t *testing.T, errs [2]error) int {
	t.Helper()
	if (errs[0] == nil) == (errs[1] == nil) {
		t.Fatalf("want one committed operation, got %v", errs)
	}
	if errs[0] == nil {
		return 0
	}
	return 1
}
