package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	accounts "github.com/trentnix/hyperserver/pkg/services/user"
)

func TestUpdateRejectsStaleSecurityState(t *testing.T) {
	for _, change := range []string{"password change", "password reset", "password update", "email", "provider", "verified", "verification required", "verification token", "changed back"} {
		t.Run(change, func(t *testing.T) {
			db := userTestDB(t)
			repository := NewAccountRepository(db)
			ctx := context.Background()
			u := &accounts.User{Email: "person@example.invalid", Password: "old hash", RegistrationAuthType: "email"}
			if err := NewAccountRepository(db).Create(ctx, u); err != nil {
				t.Fatal(err)
			}
			stale := *u
			key := []byte("test-signing-key")
			var err error
			switch change {
			case "password change":
				err = NewAccountRepository(db).ChangePassword(ctx, u, "new hash")
			case "password reset":
				token, tokenErr := accounts.NewAuthResetToken(u, key, time.Hour)
				if tokenErr != nil {
					t.Fatal(tokenErr)
				}
				if err := NewAccountRepository(db).InsertToken(t.Context(), token.Metadata()); err != nil {
					t.Fatal(err)
				}
				err = u.ResetPassword(ctx, repository, token.Token, key, "new hash")
			case "verification token":
				token, tokenErr := accounts.NewAuthVerificationToken(u, key, time.Hour)
				if tokenErr != nil {
					t.Fatal(tokenErr)
				}
				if err := NewAccountRepository(db).InsertToken(t.Context(), token.Metadata()); err != nil {
					t.Fatal(err)
				}
				u, err = accounts.Verify(ctx, NewAccountRepository(db), token.Token, key)
			default:
				switch change {
				case "password update":
					u.Password = "new hash"
				case "email":
					u.Email = "new@example.invalid"
				case "provider":
					u.RegistrationAuthType = "other-provider"
				case "verified", "changed back":
					u.Verified = true
				case "verification required":
					u.VerificationRequired = true
				}
				err = repository.Update(ctx, u)
				if err == nil && change == "changed back" {
					u.Verified = false
					err = repository.Update(ctx, u)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			current, err := NewAccountRepository(db).GetByID(t.Context(), u.ID)
			if err != nil {
				t.Fatal(err)
			}
			token, err := accounts.NewAuthVerificationToken(current, key, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			if err := NewAccountRepository(db).InsertToken(t.Context(), token.Metadata()); err != nil {
				t.Fatal(err)
			}

			// Neither saving an unchanged snapshot nor editing its email may
			// restore security state or revoke the current verification token.
			for _, email := range []string{stale.Email, "profile-edit@example.invalid"} {
				stale.Email = email
				before := stale
				if err := repository.Update(ctx, &stale); !errors.Is(err, accounts.ErrUserChanged) {
					t.Fatalf("stale update error = %v, want ErrUserChanged", err)
				}
				if stale != before {
					t.Fatal("rejected update changed the caller's snapshot")
				}
				stored, err := NewAccountRepository(db).GetByID(t.Context(), u.ID)
				if err != nil || *stored != *current {
					t.Fatalf("rejected update changed the stored account: %v", err)
				}
				if _, err := accounts.ValidateVerificationToken(context.Background(), NewAccountRepository(db), token.Token, key); err != nil {
					t.Fatalf("rejected update revoked a verification token: %v", err)
				}
			}

			// Reload and reapply only the intended edit, not the stale snapshot.
			fresh, err := NewAccountRepository(db).GetByID(t.Context(), u.ID)
			if err != nil {
				t.Fatal(err)
			}
			fresh.Email = "profile-edit@example.invalid"
			if err := repository.Update(ctx, fresh); err != nil {
				t.Fatalf("fresh update failed: %v", err)
			}
			stored, err := NewAccountRepository(db).GetByID(t.Context(), u.ID)
			if err != nil {
				t.Fatal(err)
			}
			current.Email, current.UpdatedAt = fresh.Email, stored.UpdatedAt
			current.SessionVersion++
			if *stored != *current || !fresh.UpdatedAt.Equal(stored.UpdatedAt) {
				t.Fatal("fresh update changed unexpected account fields")
			}
		})
	}
}

func TestConcurrentUpdatesRejectStaleSnapshot(t *testing.T) {
	db := userTestDB(t)
	repository := NewAccountRepository(db)
	db.SetMaxOpenConns(2)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	u := &accounts.User{Email: "person@example.invalid", Password: "original hash", RegistrationAuthType: "email"}
	if err := NewAccountRepository(db).Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	type result struct {
		account accounts.User
		err     error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for _, email := range []string{"first@example.invalid", "second@example.invalid"} {
		snapshot := *u
		snapshot.Email = email
		go func() {
			<-start
			err := repository.Update(ctx, &snapshot)
			results <- result{snapshot, err}
		}()
	}
	close(start)
	var successes int
	var winner accounts.User
	for range 2 {
		outcome := <-results
		if outcome.err == nil {
			successes++
			winner = outcome.account
		} else {
			if !errors.Is(outcome.err, accounts.ErrUserChanged) {
				t.Errorf("competing update error = %v, want ErrUserChanged", outcome.err)
			}
			if outcome.account.SessionVersion != u.SessionVersion || outcome.account.UpdatedAt != u.UpdatedAt {
				t.Error("rejected update changed the caller's snapshot")
			}
		}
	}
	stored, err := NewAccountRepository(db).GetByID(t.Context(), u.ID)
	if err != nil || successes != 1 || stored.Email != winner.Email || stored.Password != u.Password || stored.SessionVersion != 2 {
		t.Fatalf("concurrent updates: successes=%d stored=%+v error=%v", successes, stored, err)
	}
}

func TestUpdateRejectsIncorrectSecurityVersion(t *testing.T) {
	db := userTestDB(t)
	ctx := context.Background()
	u := &accounts.User{Email: "person@example.invalid", Password: "original hash"}
	if err := NewAccountRepository(db).Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	original, err := NewAccountRepository(db).GetByID(t.Context(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []int64{0, -1, u.SessionVersion + 1} {
		attempt := *u
		attempt.SessionVersion = version
		attempt.Email = "changed@example.invalid"
		before := attempt
		if err := NewAccountRepository(db).Update(ctx, &attempt); !errors.Is(err, accounts.ErrUserChanged) {
			t.Fatalf("version %d: error=%v, want ErrUserChanged", version, err)
		}
		stored, err := NewAccountRepository(db).GetByID(t.Context(), u.ID)
		if err != nil || *stored != *original || attempt != before {
			t.Fatalf("version %d: rejected update changed account state: %v", version, err)
		}
	}
}
