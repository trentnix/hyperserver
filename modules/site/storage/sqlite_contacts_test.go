package storage

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"testing/synctest"

	"github.com/trentnix/hyperserver/modules/site/models"
	"github.com/trentnix/hyperserver/pkg/services/user"
	usersqlite "github.com/trentnix/hyperserver/pkg/services/user/sqlite"
)

func contactTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "site.db"))
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

func TestSQLiteContactRepositorySharesPoolAndPreservesSchemas(t *testing.T) {
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
				if err := usersqlite.PrepareDatabase(ctx, db); err != nil {
					t.Fatal(err)
				}
			}
			repository, err := NewSQLiteContactRepository(ctx, db)
			if err != nil {
				t.Fatal(err)
			}
			submission := models.ContactSubmission{Name: "O'Neil", Email: "person@example.invalid", Message: "Hello'); DROP TABLE user; --"}
			if err := repository.Create(ctx, &submission); err != nil {
				t.Fatal(err)
			}
			if submission.Id == "" || submission.CreatedAt.IsZero() {
				t.Fatal("successful write did not supply an ID and creation time")
			}
			if err := usersqlite.PrepareDatabase(ctx, db); err != nil {
				t.Fatal(err)
			}
			account := &user.User{Email: submission.Email}
			if err := usersqlite.NewAccountRepository(db).Create(ctx, account); err != nil {
				t.Fatal(err)
			}

			// A later consumer prepares only its own schema and borrows the same pool.
			second, err := NewSQLiteContactRepository(ctx, db)
			if err != nil {
				t.Fatal(err)
			}
			if repository.db != db || second.db != db {
				t.Fatal("repositories did not retain the supplied pool")
			}
			var stored models.ContactSubmission
			if err := db.QueryRowContext(ctx, `SELECT id, name, email, message, created_at FROM hyperserver_contact_submission WHERE id = ?`, submission.Id).
				Scan(&stored.Id, &stored.Name, &stored.Email, &stored.Message, &stored.CreatedAt); err != nil {
				t.Fatal(err)
			}
			if stored.Id != submission.Id || stored.Name != submission.Name || stored.Email != submission.Email || stored.Message != submission.Message || !stored.CreatedAt.Equal(submission.CreatedAt) {
				t.Fatalf("stored submission changed: %+v", stored)
			}
			if _, err := usersqlite.NewAccountRepository(db).GetByID(t.Context(), account.ID); err != nil {
				t.Fatal("site initialization affected account storage:", err)
			}
		})
	}
}

func TestSQLiteContactRepositorySetupFailureLeavesPoolOpen(t *testing.T) {
	db := contactTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if repository, err := NewSQLiteContactRepository(ctx, db); repository != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled setup = %v, %v", repository, err)
	}
	if err := db.Ping(); err != nil {
		t.Fatal("failed initialization closed the borrowed pool:", err)
	}
	if _, err := NewSQLiteContactRepository(context.Background(), db); err != nil {
		t.Fatal("retry after cancellation:", err)
	}
	if repository, err := NewSQLiteContactRepository(context.Background(), nil); repository != nil || err == nil {
		t.Fatalf("nil database setup = %v, %v", repository, err)
	}
}

func TestSQLiteContactRepositoryCreateFailures(t *testing.T) {
	for _, failure := range []string{"invalid email", "canceled request", "storage failure", "missing schema"} {
		t.Run(failure, func(t *testing.T) {
			db := contactTestDB(t)
			repository, err := NewSQLiteContactRepository(context.Background(), db)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			submission := models.ContactSubmission{Email: "person@example.invalid", Message: "Hello"}
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
			err = repository.Create(ctx, &submission)
			if err == nil {
				t.Fatal("invalid or failed write succeeded")
			}
			if failure == "canceled request" && !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled write returned %v, want context.Canceled", err)
			}
			if submission != before {
				t.Fatal("failed write changed the caller's submission")
			}
			if failure == "missing schema" {
				var count int
				if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM sqlite_master WHERE name = 'hyperserver_contact_submission'`).Scan(&count); err != nil || count != 0 {
					t.Fatalf("request recreated the schema: count = %d, error = %v", count, err)
				}
			} else {
				var count int
				if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM hyperserver_contact_submission`).Scan(&count); err != nil || count != 0 {
					t.Fatalf("failed write persisted data: count = %d, error = %v", count, err)
				}
			}
		})
	}
}

func TestSQLiteContactRepositoryCreateCancellationWhileWaiting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		db := contactTestDB(t)
		repository, err := NewSQLiteContactRepository(t.Context(), db)
		if err != nil {
			t.Fatal(err)
		}
		// Hold the only connection so the write must wait for its request context.
		conn, err := db.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		submission := models.ContactSubmission{Name: "Waiting", Email: "person@example.invalid", Message: "Not stored"}
		before := submission
		result := make(chan error, 1)
		go func() { result <- repository.Create(ctx, &submission) }()
		synctest.Wait()
		select {
		case err := <-result:
			t.Fatalf("write did not wait for the connection: %v", err)
		default:
		}

		cancel()
		synctest.Wait()
		select {
		case err = <-result:
		default:
			// Release a broken implementation before reporting the failure.
			conn.Close()
			<-result
			t.Fatal("request cancellation did not interrupt the write")
		}
		if !errors.Is(err, context.Canceled) || submission != before {
			t.Fatalf("canceled write changed its input or lost cancellation: %v", err)
		}
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}

		var count int
		if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM hyperserver_contact_submission`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("canceled write persisted data: count=%d, error=%v", count, err)
		}
	})
}

func TestSQLiteContactRepositoryRejectsNilSubmission(t *testing.T) {
	db := contactTestDB(t)
	repository, err := NewSQLiteContactRepository(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Create(t.Context(), nil); err == nil {
		t.Fatal("nil submission succeeded")
	}
	var count int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM hyperserver_contact_submission`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("nil submission persisted data: count=%d, error=%v", count, err)
	}
}

func TestSQLiteContactRepositorySchemaFailurePreservesSharedStorage(t *testing.T) {
	db := contactTestDB(t)
	ctx := context.Background()
	if err := usersqlite.PrepareDatabase(ctx, db); err != nil {
		t.Fatal(err)
	}
	account := &user.User{Email: "person@example.invalid", Password: "stored hash", Verified: true}
	if err := usersqlite.NewAccountRepository(db).Create(ctx, account); err != nil {
		t.Fatal(err)
	}
	before, err := usersqlite.NewAccountRepository(db).GetByID(t.Context(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	// SQLite cannot create a table with the same name as an existing index.
	if _, err := db.Exec(`CREATE INDEX hyperserver_contact_submission ON user(email)`); err != nil {
		t.Fatal(err)
	}
	if repository, err := NewSQLiteContactRepository(ctx, db); repository != nil || err == nil {
		t.Fatalf("conflicting schema setup = %v, %v, want no repository and an error", repository, err)
	}

	if err := db.PingContext(ctx); err != nil {
		t.Fatal("schema failure closed the shared pool:", err)
	}
	stored, err := usersqlite.NewAccountRepository(db).GetByID(t.Context(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if *stored != *before {
		t.Fatal("schema failure changed the existing account")
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'hyperserver_contact_submission'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed setup created a contact table: count = %d, error = %v", count, err)
	}
	if err := usersqlite.NewAccountRepository(db).Create(ctx, &user.User{Email: "another@example.invalid"}); err != nil {
		t.Fatal("account writes failed after site setup failure:", err)
	}

	if _, err := db.Exec(`DROP INDEX hyperserver_contact_submission`); err != nil {
		t.Fatal(err)
	}
	repository, err := NewSQLiteContactRepository(ctx, db)
	if err != nil {
		t.Fatal("retry after resolving the schema conflict:", err)
	}
	if err := repository.Create(ctx, &models.ContactSubmission{Email: account.Email, Message: "Retry succeeded"}); err != nil {
		t.Fatal(err)
	}
}
