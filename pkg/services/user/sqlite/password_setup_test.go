package sqlite

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestSetupPassword(t *testing.T) {
	for _, scenario := range []string{"success", "recovery token", "wrong recipient", "changed email", "changed version", "existing password", "unverified", "stored expiry", "supplied expiry", "storage failure", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			db := userTestDB(t)
			repo := NewAccountRepository(db)
			u := &user.User{Email: "owner@example.com", Verified: true, VerificationRequired: true}
			if err := repo.Create(t.Context(), u); err != nil {
				t.Fatal(err)
			}
			candidate := *u
			token, err := user.NewPasswordSetupToken(u, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch scenario {
			case "recovery token":
				token.Type = user.ResetTokenPurpose
			case "wrong recipient":
				token.Email = "someone@example.com"
			case "changed email":
				u.Email = "new@example.com"
			case "changed version":
				candidate.SessionVersion--
			case "existing password":
				u.Password = "existing hash"
			case "unverified":
				u.Verified = false
			case "stored expiry":
				token.ExpiresAt = time.Now().Add(-time.Hour)
			case "storage failure":
				if _, err := db.Exec(`CREATE TRIGGER deny_setup BEFORE UPDATE ON user BEGIN SELECT RAISE(ABORT, 'unavailable'); END`); err != nil {
					t.Fatal(err)
				}
			case "canceled":
				cancel()
			}
			if scenario == "changed email" || scenario == "existing password" || scenario == "unverified" {
				if err := repo.Update(t.Context(), u); err != nil {
					t.Fatal(err)
				}
			}
			if err := repo.InsertToken(t.Context(), token.Metadata()); err != nil {
				t.Fatal(err)
			}
			authorization := token.Metadata()
			// Forging supplied purpose or expiry must not bypass stored authorization.
			authorization.Purpose = user.PasswordSetupTokenPurpose
			if scenario == "stored expiry" {
				authorization.ExpiresAt = time.Now().Add(time.Hour)
			}
			if scenario == "supplied expiry" {
				authorization.ExpiresAt = time.Now().Add(-time.Hour)
			}
			before := candidate
			err = repo.SetupPassword(ctx, &candidate, authorization, "new hash")
			stored, readErr := repo.GetByID(t.Context(), u.ID)
			if readErr != nil {
				t.Fatal(readErr)
			}
			_, tokenErr := repo.GetToken(t.Context(), token.TokenHash, token.Type)
			if scenario == "success" {
				if err != nil || stored.Password != "new hash" || stored.SessionVersion != u.SessionVersion+1 || !sameUnlinkAccount(candidate, *stored) || !errors.Is(tokenErr, user.ErrTokenNotFound) {
					t.Fatal("incorrect enrollment", err, tokenErr)
				}
			} else if err == nil || tokenErr != nil || candidate != before || !sameUnlinkAccount(*u, *stored) {
				t.Fatal("failed enrollment changed account or token", err, tokenErr)
			}
		})
	}
}

func TestConcurrentPasswordSetup(t *testing.T) {
	repo := NewAccountRepository(userTestDB(t))
	u := &user.User{Email: "owner@example.com", Verified: true}
	if err := repo.Create(t.Context(), u); err != nil {
		t.Fatal(err)
	}
	var tokens [2]user.TokenMetadata
	for i := range tokens {
		token, err := user.NewPasswordSetupToken(u, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		tokens[i] = token.Metadata()
		if err := repo.InsertToken(t.Context(), tokens[i]); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	var results [2]error
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			candidate := *u
			results[i] = repo.SetupPassword(t.Context(), &candidate, tokens[i], "hash")
		}()
	}
	wg.Wait()
	successes := 0
	for _, err := range results {
		if err == nil {
			successes++
		} else if !user.IsTokenRejection(err) {
			t.Fatal(err)
		}
	}
	stored, err := repo.GetByID(t.Context(), u.ID)
	if err != nil || successes != 1 || stored.SessionVersion != u.SessionVersion+1 {
		t.Fatal("setup did not commit exactly once", results, err)
	}
}

func TestResetPasswordRejectsSetupToken(t *testing.T) {
	repo := NewAccountRepository(userTestDB(t))
	u := &user.User{Email: "owner@example.com", Password: "existing hash", Verified: true}
	if err := repo.Create(t.Context(), u); err != nil {
		t.Fatal(err)
	}
	token, err := user.NewPasswordSetupToken(u, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.InsertToken(t.Context(), token.Metadata()); err != nil {
		t.Fatal(err)
	}
	before := *u
	err = repo.ResetPassword(t.Context(), u, user.ResetAuthorization{AccountID: u.ID, TokenHash: token.TokenHash, ExpiresAt: token.ExpiresAt}, "replacement")
	if !errors.Is(err, user.ErrTokenNotFound) || *u != before {
		t.Fatal("reset accepted a setup token", err)
	}
	stored, err := repo.GetByID(t.Context(), u.ID)
	if err != nil || !sameUnlinkAccount(*stored, before) {
		t.Fatal("reset changed stored credentials", err)
	}
	if _, err := repo.GetToken(t.Context(), token.TokenHash, user.PasswordSetupTokenPurpose); err != nil {
		t.Fatal("reset consumed setup token", err)
	}
}
