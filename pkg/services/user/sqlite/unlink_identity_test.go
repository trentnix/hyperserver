package sqlite

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestUnlinkExternalIdentity(t *testing.T) {
	for _, scenario := range []string{"success", "no password", "unverified", "stale version", "changed password", "missing link", "storage failure", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			db := userTestDB(t)
			repo := NewAccountRepository(db)
			u := &user.User{Email: "owner@example.com", Password: "proven hash", Verified: true, VerificationRequired: true}
			id := user.ExternalIdentity{Issuer: "google", Subject: "owner"}
			if err := repo.CreateExternalAccount(t.Context(), u, id); err != nil {
				t.Fatal(err)
			}
			other := &user.User{Email: "other@example.com", Password: "other hash", Verified: true}
			if err := repo.CreateExternalAccount(t.Context(), other, user.ExternalIdentity{Issuer: id.Issuer, Subject: "other"}); err != nil {
				t.Fatal(err)
			}
			candidate := *u
			issuer := id.Issuer
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch scenario {
			case "no password":
				candidate.Password = ""
				if err := repo.Update(ctx, &candidate); err != nil {
					t.Fatal(err)
				}
				*u = candidate
			case "unverified":
				candidate.Verified = false
			case "stale version":
				candidate.SessionVersion++
			case "changed password":
				u.Password = "new hash"
				if err := repo.Update(ctx, u); err != nil {
					t.Fatal(err)
				}
			case "missing link":
				issuer = "missing"
			case "storage failure":
				if _, err := db.Exec(`CREATE TRIGGER deny_unlink BEFORE DELETE ON external_identity BEGIN SELECT RAISE(ABORT, 'unavailable'); END`); err != nil {
					t.Fatal(err)
				}
			case "canceled":
				cancel()
			}
			before := candidate
			err := repo.UnlinkExternalIdentity(ctx, &candidate, issuer)
			stored, readErr := repo.GetByID(t.Context(), u.ID)
			if readErr != nil {
				t.Fatal(readErr)
			}
			linked, readErr := repo.HasExternalIdentity(t.Context(), u.ID, id.Issuer)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if scenario == "success" || scenario == "no password" {
				if err != nil || linked || candidate.SessionVersion != u.SessionVersion+1 || !sameUnlinkAccount(candidate, *stored) || stored.Password != u.Password {
					t.Fatal("incorrect unlink", err)
				}
				if err := repo.UnlinkExternalIdentity(t.Context(), &candidate, issuer); err == nil {
					t.Fatal("repeated disconnect succeeded")
				}
			} else if err == nil || !linked || !sameUnlinkAccount(*stored, *u) || candidate != before {
				t.Fatal("failure changed account or link", err)
			}
			otherStored, err := repo.GetExternalAccount(t.Context(), user.ExternalIdentity{Issuer: id.Issuer, Subject: "other"})
			if err != nil || !sameUnlinkAccount(*otherStored, *other) {
				t.Fatal("disconnect changed another account", err)
			}
		})
	}
}

func sameUnlinkAccount(a, b user.User) bool {
	a.CreatedAt, b.CreatedAt = a.CreatedAt.Round(0).UTC(), b.CreatedAt.Round(0).UTC()
	a.UpdatedAt, b.UpdatedAt = a.UpdatedAt.Round(0).UTC(), b.UpdatedAt.Round(0).UTC()
	return a == b
}

func TestConcurrentUnlinkExternalIdentity(t *testing.T) {
	repo := NewAccountRepository(userTestDB(t))
	u := &user.User{Email: "owner@example.com", Password: "hash", Verified: true}
	if err := repo.CreateExternalAccount(t.Context(), u, user.ExternalIdentity{Issuer: "google", Subject: "owner"}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make([]error, 2)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			candidate := *u
			results[i] = repo.UnlinkExternalIdentity(t.Context(), &candidate, "google")
		}()
	}
	wg.Wait()
	successes := 0
	for _, err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, user.ErrUserChanged) {
			t.Fatal(err)
		}
	}
	stored, err := repo.GetByID(t.Context(), u.ID)
	if err != nil || successes != 1 || stored.SessionVersion != u.SessionVersion+1 {
		t.Fatal("concurrent disconnect did not commit exactly once", results, err)
	}
}
