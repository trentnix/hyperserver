package user

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/pkg/database"
)

// These tests are sequential because schema initialization still uses package
// state. Reset that state only when no test operations are running.
func userTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db, err := sqlx.Open("sqlite3", filepath.Join(t.TempDir(), "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	dbMutex.Lock()
	databaseConfigured = false
	dbMutex.Unlock()
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
		dbMutex.Lock()
		databaseConfigured = false
		dbMutex.Unlock()
	})
	return db
}

func TestCreateRejectsStaleRegistration(t *testing.T) {
	db := userTestDB(t)
	if _, err := GetUserByEmail(db, "person@example.invalid"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("initial account lookup = %v, want no rows", err)
	}
	// Both callers saw an unused email. One finishes before the other persists.
	winner := &User{Email: "person@example.invalid", Password: "winning hash", Verified: true}
	loser := &User{Email: winner.Email, Password: "losing hash", VerificationRequired: true}
	if err := winner.Create(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	original, err := GetUserByID(db, winner.ID)
	if err != nil {
		t.Fatal(err)
	}
	before := *loser
	var duplicate *database.ErrRecordAlreadyExists
	if err := loser.Create(context.Background(), db); !errors.As(err, &duplicate) {
		t.Errorf("stale registration error = %v, want duplicate email", err)
	}
	if *loser != before {
		t.Error("failed creation changed the caller's user")
	}
	stored, err := GetUserByID(db, winner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if *stored != *original {
		t.Error("stale registration overwrote the existing account")
	}
}

func TestUpdateRejectsMissingUser(t *testing.T) {
	db := userTestDB(t)
	u := &User{ID: "missing-id", Email: "person@example.invalid", Password: "hash"}
	before := *u
	var missing *ErrUserNotFound
	if err := u.Update(context.Background(), db); !errors.As(err, &missing) {
		t.Errorf("missing account error = %v, want ErrUserNotFound", err)
	}
	if *u != before {
		t.Error("failed update changed the caller's user")
	}
	if _, err := GetUserByEmail(db, u.Email); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("updating a missing account created one: %v", err)
	}
}

func TestUpdateUsesID(t *testing.T) {
	db := userTestDB(t)
	ctx := context.Background()
	u := &User{Email: "before@example.invalid", Password: "original hash"}
	if err := u.Create(ctx, db); err != nil {
		t.Fatal(err)
	}
	id, createdAt := u.ID, u.CreatedAt
	u.Email = "after@example.invalid"
	u.Password = "updated hash"
	u.Verified = true
	if err := u.Update(ctx, db); err != nil {
		t.Fatal(err)
	}
	stored, err := GetUserByID(db, id)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Email != u.Email || stored.Password != u.Password || !stored.Verified || !stored.CreatedAt.Equal(createdAt) {
		t.Error("update did not preserve identity and apply the changes")
	}
	if _, err := GetUserByEmail(db, "before@example.invalid"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("old email still exists: %v", err)
	}
	var count int
	if err := db.Get(&count, "SELECT COUNT(*) FROM user"); err != nil || count != 1 {
		t.Fatalf("update created another row: count=%d, error=%v", count, err)
	}
	if err := u.Delete(db); err != nil {
		t.Fatal(err)
	}
	if err := u.Update(ctx, db); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("updating a deleted user = %v, want not found", err)
	}
}

func TestUpdateRejectsEmailConflict(t *testing.T) {
	db := userTestDB(t)
	ctx := context.Background()
	first := &User{Email: "first@example.invalid", Password: "first hash"}
	second := &User{Email: "second@example.invalid", Password: "second hash"}
	for _, u := range []*User{first, second} {
		if err := u.Create(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	first.Email = second.Email
	before := *first
	var duplicate *database.ErrRecordAlreadyExists
	if err := first.Update(ctx, db); !errors.As(err, &duplicate) {
		t.Fatalf("email conflict = %v, want duplicate email", err)
	}
	if *first != before {
		t.Error("failed update changed the caller's user")
	}
	for _, tc := range []struct{ id, email, password string }{
		{first.ID, "first@example.invalid", "first hash"},
		{second.ID, "second@example.invalid", "second hash"},
	} {
		stored, err := GetUserByID(db, tc.id)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Email != tc.email || stored.Password != tc.password {
			t.Error("email conflict changed an account")
		}
	}
}

func TestUserWritesRejectInvalidInput(t *testing.T) {
	for _, tc := range []struct {
		name string
		u    *User
	}{
		{"nil user", nil},
		{"empty user", &User{}},
		{"id without email", &User{ID: "existing-id"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := userTestDB(t)
			if err := tc.u.Create(context.Background(), db); err == nil {
				t.Error("Create accepted invalid input")
			}
			if err := tc.u.Update(context.Background(), db); err == nil {
				t.Error("Update accepted invalid input")
			}
		})
	}
	t.Run("identified user cannot be created", func(t *testing.T) {
		db := userTestDB(t)
		u := &User{ID: "existing-id", Email: "person@example.invalid"}
		if err := u.Create(context.Background(), db); err == nil {
			t.Error("Create accepted an existing ID")
		}
	})
	t.Run("email alone cannot select an update target", func(t *testing.T) {
		db := userTestDB(t)
		u := &User{Email: "person@example.invalid"}
		if err := u.Update(context.Background(), db); err == nil {
			t.Error("Update accepted a missing ID")
		}
	})
}

func TestUserWritesPropagateFailures(t *testing.T) {
	for _, operation := range []string{"create", "update"} {
		for _, failure := range []string{"nil database", "closed database", "canceled context"} {
			t.Run(operation+"/"+failure, func(t *testing.T) {
				db := userTestDB(t)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				u := &User{Email: "person@example.invalid", Password: "hash"}
				if operation == "update" {
					if err := u.Create(ctx, db); err != nil {
						t.Fatal(err)
					}
				}
				before := *u
				target := db
				switch failure {
				case "nil database":
					target = nil
				case "closed database":
					if err := db.Close(); err != nil {
						t.Fatal(err)
					}
				case "canceled context":
					cancel()
				}
				var err error
				if operation == "create" {
					err = u.Create(ctx, target)
				} else {
					err = u.Update(ctx, target)
				}
				if err == nil {
					t.Fatal("write failure was ignored")
				}
				if failure == "canceled context" && !errors.Is(err, context.Canceled) {
					t.Errorf("error = %v, want context.Canceled", err)
				}
				if *u != before {
					t.Error("failed write changed the caller's user")
				}
			})
		}
	}
}
