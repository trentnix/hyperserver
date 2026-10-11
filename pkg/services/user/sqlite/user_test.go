package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/jmoiron/sqlx"
	accounts "github.com/trentnix/hyperserver/pkg/services/user"
)

func userTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db := unpreparedUserTestDB(t)
	if err := PrepareDatabase(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db
}

func unpreparedUserTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db, err := sqlx.Open("sqlite3", filepath.Join(t.TempDir(), "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return db
}

func TestSQLiteAccountRepositoryGetByEmail(t *testing.T) {
	db := userTestDB(t)
	u := &accounts.User{Email: "person@example.invalid"}
	if err := NewAccountRepository(db).Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	got, err := NewAccountRepository(db).GetByEmail(context.Background(), u.Email)
	if err != nil || got.ID != u.ID {
		t.Fatalf("account lookup = %v, %v", got, err)
	}
	if _, err := NewAccountRepository(db).GetByEmail(context.Background(), "unknown@example.invalid"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unknown account error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewAccountRepository(db).GetByEmail(ctx, u.Email); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled lookup error = %v", err)
	}
	if _, err := NewAccountRepository(nil).GetByEmail(context.Background(), u.Email); err == nil {
		t.Fatal("missing database did not fail")
	}
	if _, err := NewAccountRepository(db).GetByEmail(context.Background(), ""); err == nil {
		t.Fatal("empty email did not fail")
	}
}

func TestCreateRejectsStaleRegistration(t *testing.T) {
	db := userTestDB(t)
	if _, err := NewAccountRepository(db).GetByEmail(t.Context(), "person@example.invalid"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("initial account lookup = %v, want no rows", err)
	}
	// Both callers saw an unused email. One finishes before the other persists.
	winner := &accounts.User{Email: "person@example.invalid", Password: "winning hash", Verified: true}
	loser := &accounts.User{Email: winner.Email, Password: "losing hash", VerificationRequired: true}
	if err := NewAccountRepository(db).Create(context.Background(), winner); err != nil {
		t.Fatal(err)
	}
	original, err := NewAccountRepository(db).GetByID(t.Context(), winner.ID)
	if err != nil {
		t.Fatal(err)
	}
	before := *loser
	if err := NewAccountRepository(db).Create(context.Background(), loser); !errors.Is(err, accounts.ErrEmailAlreadyExists) {
		t.Errorf("stale registration error = %v, want duplicate email", err)
	}
	if *loser != before {
		t.Error("failed creation changed the caller's user")
	}
	stored, err := NewAccountRepository(db).GetByID(t.Context(), winner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if *stored != *original {
		t.Error("stale registration overwrote the existing account")
	}
}

func TestUpdateRejectsMissingUser(t *testing.T) {
	db := userTestDB(t)
	u := &accounts.User{ID: "missing-id", Email: "person@example.invalid", Password: "hash"}
	before := *u
	var missing *accounts.ErrUserNotFound
	if err := NewAccountRepository(db).Update(context.Background(), u); !errors.As(err, &missing) {
		t.Errorf("missing account error = %v, want ErrUserNotFound", err)
	}
	if *u != before {
		t.Error("failed update changed the caller's user")
	}
	if _, err := NewAccountRepository(db).GetByEmail(t.Context(), u.Email); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("updating a missing account created one: %v", err)
	}
}

func TestUpdateUsesID(t *testing.T) {
	db := userTestDB(t)
	ctx := context.Background()
	u := &accounts.User{Email: "before@example.invalid", Password: "original hash"}
	if err := NewAccountRepository(db).Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	id, createdAt := u.ID, u.CreatedAt
	u.Email = "after@example.invalid"
	u.Password = "updated hash"
	u.Verified = true
	if err := NewAccountRepository(db).Update(ctx, u); err != nil {
		t.Fatal(err)
	}
	stored, err := NewAccountRepository(db).GetByID(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Email != u.Email || stored.Password != u.Password || !stored.Verified || !stored.CreatedAt.Equal(createdAt) {
		t.Error("update did not preserve identity and apply the changes")
	}
	if _, err := NewAccountRepository(db).GetByEmail(t.Context(), "before@example.invalid"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("old email still exists: %v", err)
	}
	var count int
	if err := db.Get(&count, "SELECT COUNT(*) FROM user"); err != nil || count != 1 {
		t.Fatalf("update created another row: count=%d, error=%v", count, err)
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM user WHERE id = ?", u.ID); err != nil {
		t.Fatal(err)
	}
	if err := NewAccountRepository(db).Update(ctx, u); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("updating a deleted user = %v, want not found", err)
	}
}

func TestUpdateRejectsEmailConflict(t *testing.T) {
	db := userTestDB(t)
	ctx := context.Background()
	first := &accounts.User{Email: "first@example.invalid", Password: "first hash"}
	second := &accounts.User{Email: "second@example.invalid", Password: "second hash"}
	for _, u := range []*accounts.User{first, second} {
		if err := NewAccountRepository(db).Create(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	first.Email = second.Email
	before := *first
	if err := NewAccountRepository(db).Update(ctx, first); !errors.Is(err, accounts.ErrEmailAlreadyExists) {
		t.Fatalf("email conflict = %v, want duplicate email", err)
	}
	if *first != before {
		t.Error("failed update changed the caller's user")
	}
	for _, tc := range []struct{ id, email, password string }{
		{first.ID, "first@example.invalid", "first hash"},
		{second.ID, "second@example.invalid", "second hash"},
	} {
		stored, err := NewAccountRepository(db).GetByID(t.Context(), tc.id)
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
		u    *accounts.User
	}{
		{"nil user", nil},
		{"empty user", &accounts.User{}},
		{"id without email", &accounts.User{ID: "existing-id"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := userTestDB(t)
			if err := NewAccountRepository(db).Create(context.Background(), tc.u); err == nil {
				t.Error("Create accepted invalid input")
			}
			if err := NewAccountRepository(db).Update(context.Background(), tc.u); err == nil {
				t.Error("Update accepted invalid input")
			}
		})
	}
	t.Run("identified user cannot be created", func(t *testing.T) {
		db := userTestDB(t)
		u := &accounts.User{ID: "existing-id", Email: "person@example.invalid"}
		if err := NewAccountRepository(db).Create(context.Background(), u); err == nil {
			t.Error("Create accepted an existing ID")
		}
	})
	t.Run("email alone cannot select an update target", func(t *testing.T) {
		db := userTestDB(t)
		u := &accounts.User{Email: "person@example.invalid"}
		if err := NewAccountRepository(db).Update(context.Background(), u); err == nil {
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
				u := &accounts.User{Email: "person@example.invalid", Password: "hash"}
				if operation == "update" {
					if err := NewAccountRepository(db).Create(ctx, u); err != nil {
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
					err = NewAccountRepository(target).Create(ctx, u)
				} else {
					err = NewAccountRepository(target).Update(ctx, u)
				}
				if err == nil {
					t.Fatal("write failure was ignored")
				}
				if errors.Is(err, accounts.ErrEmailAlreadyExists) {
					t.Fatalf("write failure was classified as a duplicate email: %v", err)
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
