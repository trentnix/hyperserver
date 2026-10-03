package models

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func contactTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db, err := sqlx.Open("sqlite3", filepath.Join(t.TempDir(), "site.db"))
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

func TestContactRepositorySharesPoolAndPreservesSchemas(t *testing.T) {
	for _, tc := range []struct {
		name      string
		siteFirst bool
	}{
		{name: "accounts first"},
		{name: "site first", siteFirst: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := contactTestDB(t)
			ctx := context.Background()
			if !tc.siteFirst {
				if err := user.PrepareDatabase(ctx, db); err != nil {
					t.Fatal(err)
				}
			}
			repository, err := NewContactRepository(ctx, db)
			if err != nil {
				t.Fatal(err)
			}
			submission := ContactSubmission{Name: "Test person", Email: "person@example.invalid", Message: "Hello"}
			if err := repository.Create(ctx, &submission); err != nil {
				t.Fatal(err)
			}
			if err := user.PrepareDatabase(ctx, db); err != nil {
				t.Fatal(err)
			}
			account := &user.User{Email: submission.Email}
			if err := account.Create(ctx, db); err != nil {
				t.Fatal(err)
			}

			// A later consumer prepares only its own schema and borrows the same pool.
			second, err := NewContactRepository(ctx, db)
			if err != nil {
				t.Fatal(err)
			}
			if repository.db != db || second.db != db {
				t.Fatal("repositories did not retain the supplied pool")
			}
			var stored ContactSubmission
			if err := db.Get(&stored, `SELECT * FROM hyperserver_contact_submission WHERE id = ?`, submission.Id); err != nil {
				t.Fatal(err)
			}
			if stored.Id != submission.Id || stored.Message != submission.Message || !stored.CreatedAt.Equal(submission.CreatedAt) {
				t.Fatalf("stored submission changed: %+v", stored)
			}
			if _, err := user.GetUserByID(db, account.ID); err != nil {
				t.Fatal("site initialization affected account storage:", err)
			}
		})
	}
}

func TestContactRepositorySetupFailureLeavesPoolOpen(t *testing.T) {
	db := contactTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if repository, err := NewContactRepository(ctx, db); repository != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled setup = %v, %v", repository, err)
	}
	if err := db.Ping(); err != nil {
		t.Fatal("failed initialization closed the borrowed pool:", err)
	}
	if _, err := NewContactRepository(context.Background(), db); err != nil {
		t.Fatal("retry after cancellation:", err)
	}
	if repository, err := NewContactRepository(context.Background(), nil); repository != nil || err == nil {
		t.Fatalf("nil database setup = %v, %v", repository, err)
	}
}

func TestContactRepositoryCreateFailures(t *testing.T) {
	for _, failure := range []string{"invalid email", "canceled request", "storage failure", "missing schema"} {
		t.Run(failure, func(t *testing.T) {
			db := contactTestDB(t)
			repository, err := NewContactRepository(context.Background(), db)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			submission := ContactSubmission{Email: "person@example.invalid", Message: "Hello"}
			switch failure {
			case "invalid email":
				submission.Email = "invalid"
			case "canceled request":
				cancel()
			case "storage failure":
				if _, err := db.Exec(`CREATE TRIGGER fail_contact BEFORE INSERT ON hyperserver_contact_submission BEGIN SELECT RAISE(ABORT, 'failed insertion'); END`); err != nil {
					t.Fatal(err)
				}
			case "missing schema":
				if _, err := db.Exec(`DROP TABLE hyperserver_contact_submission`); err != nil {
					t.Fatal(err)
				}
			}
			before := submission
			if err := repository.Create(ctx, &submission); err == nil {
				t.Fatal("invalid or failed write succeeded")
			}
			if submission != before {
				t.Fatal("failed write changed the caller's submission")
			}
			if failure == "missing schema" {
				var count int
				if err := db.Get(&count, `SELECT count(*) FROM sqlite_master WHERE name = 'hyperserver_contact_submission'`); err != nil || count != 0 {
					t.Fatalf("request recreated the schema: count = %d, error = %v", count, err)
				}
			} else {
				var count int
				if err := db.Get(&count, `SELECT count(*) FROM hyperserver_contact_submission`); err != nil || count != 0 {
					t.Fatalf("failed write persisted data: count = %d, error = %v", count, err)
				}
			}
		})
	}
}

func TestContactRepositorySchemaFailurePreservesSharedStorage(t *testing.T) {
	db := contactTestDB(t)
	ctx := context.Background()
	if err := user.PrepareDatabase(ctx, db); err != nil {
		t.Fatal(err)
	}
	account := &user.User{Email: "person@example.invalid", Password: "stored hash", Verified: true}
	if err := account.Create(ctx, db); err != nil {
		t.Fatal(err)
	}
	before, err := user.GetUserByID(db, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	// SQLite cannot create a table with the same name as an existing index.
	if _, err := db.Exec(`CREATE INDEX hyperserver_contact_submission ON user(email)`); err != nil {
		t.Fatal(err)
	}
	if repository, err := NewContactRepository(ctx, db); repository != nil || err == nil {
		t.Fatalf("conflicting schema setup = %v, %v, want no repository and an error", repository, err)
	}

	if err := db.PingContext(ctx); err != nil {
		t.Fatal("schema failure closed the shared pool:", err)
	}
	stored, err := user.GetUserByID(db, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if *stored != *before {
		t.Fatal("schema failure changed the existing account")
	}
	var count int
	if err := db.Get(&count, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'hyperserver_contact_submission'`); err != nil || count != 0 {
		t.Fatalf("failed setup created a contact table: count = %d, error = %v", count, err)
	}
	if err := (&user.User{Email: "another@example.invalid"}).Create(ctx, db); err != nil {
		t.Fatal("account writes failed after site setup failure:", err)
	}

	if _, err := db.Exec(`DROP INDEX hyperserver_contact_submission`); err != nil {
		t.Fatal(err)
	}
	repository, err := NewContactRepository(ctx, db)
	if err != nil {
		t.Fatal("retry after resolving the schema conflict:", err)
	}
	if err := repository.Create(ctx, &ContactSubmission{Email: account.Email, Message: "Retry succeeded"}); err != nil {
		t.Fatal(err)
	}
}
