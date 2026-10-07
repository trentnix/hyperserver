package user

import (
	"context"
	"testing"
)

func TestPrepareDatabaseAddsSessionVersion(t *testing.T) {
	db := userTestDB(t)
	u := &User{Email: "legacy@example.invalid", Password: "preserved"}
	if err := u.Create(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	u, err := GetUserByID(db, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE user DROP COLUMN session_version`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := PrepareDatabase(context.Background(), db); err != nil {
			t.Fatal(err)
		}
		stored, err := GetUserByID(db, u.ID)
		if err != nil || *stored != *u {
			t.Fatalf("account changed during session version setup: %+v, %v", stored, err)
		}
	}
}

func TestAccountChangesAdvanceSessionVersion(t *testing.T) {
	for _, field := range []string{"password", "email", "verified", "verification required", "provider", "unchanged"} {
		t.Run(field, func(t *testing.T) {
			db := userTestDB(t)
			u := &User{Email: "person@example.invalid", Password: "old hash", RegistrationAuthType: "email"}
			if err := u.Create(context.Background(), db); err != nil {
				t.Fatal(err)
			}
			switch field {
			case "password":
				u.Password = "new hash"
			case "email":
				u.Email = "updated@example.invalid"
			case "verified":
				u.Verified = true
			case "verification required":
				u.VerificationRequired = true
			case "provider":
				u.RegistrationAuthType = "another-provider"
			}
			if err := NewSQLiteAccountRepository(db).Update(context.Background(), u); err != nil {
				t.Fatal(err)
			}
			want := int64(2)
			if field == "unchanged" {
				want = 1
			}
			stored, err := GetUserByID(db, u.ID)
			if err != nil || stored.SessionVersion != want || u.SessionVersion != want {
				t.Fatalf("version = %d, want %d: %v", u.SessionVersion, want, err)
			}
		})
	}
}

func TestSQLiteAccountRepositoryChangePassword(t *testing.T) {
	for _, outcome := range []string{"success", "stale password", "stale provider", "stale version", "storage failure", "canceled", "empty hash", "missing account"} {
		t.Run(outcome, func(t *testing.T) {
			db := userTestDB(t)
			u := &User{Email: "password@example.invalid", Password: "old hash", RegistrationAuthType: "email"}
			if err := u.Create(context.Background(), db); err != nil {
				t.Fatal(err)
			}
			u, err := GetUserByID(db, u.ID)
			if err != nil {
				t.Fatal(err)
			}
			original := *u
			ctx := context.Background()
			hash := "new hash"
			switch outcome {
			case "stale password":
				u.Password = "stale"
			case "stale provider":
				u.RegistrationAuthType = "another-provider"
			case "stale version":
				u.SessionVersion--
			case "storage failure":
				if _, err := db.Exec(`CREATE TRIGGER reject_change BEFORE UPDATE ON user BEGIN SELECT RAISE(ABORT, 'write failed'); END`); err != nil {
					t.Fatal(err)
				}
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "empty hash":
				hash = ""
			case "missing account":
				u.ID = "missing"
			}
			before := *u
			err = NewSQLiteAccountRepository(db).ChangePassword(ctx, u, hash)
			if (err == nil) != (outcome == "success") {
				t.Fatalf("password change error = %v", err)
			}
			stored, err := GetUserByID(db, original.ID)
			if err != nil {
				t.Fatal(err)
			}
			if outcome == "success" {
				original.Password, original.UpdatedAt = hash, stored.UpdatedAt
				original.SessionVersion++
				expected := *u
				expected.UpdatedAt = stored.UpdatedAt
				if !u.UpdatedAt.Equal(stored.UpdatedAt) || expected != *stored {
					t.Fatal("receiver differs from committed account")
				}
			} else if *u != before {
				t.Fatal("failed operation mutated receiver")
			}
			if *stored != original {
				t.Fatal("password change altered unexpected account fields")
			}
		})
	}
}
