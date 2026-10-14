package storage

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/internal/test/contacttest"
	"github.com/trentnix/hyperserver/modules/site/models"
)

func TestSQLiteContactContract(t *testing.T) {
	contacttest.Run(t, func(t *testing.T) contacttest.Fixture {
		db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "contacts.db")+"?_journal=WAL&_timeout=5000&_fk=true")
		if err != nil {
			t.Fatal(err)
		}
		// Competing creates must also work across separate SQLite connections.
		db.SetMaxOpenConns(2)
		t.Cleanup(func() {
			if err := db.Close(); err != nil {
				t.Error(err)
			}
		})
		repo, err := NewSQLiteContactRepository(t.Context(), db)
		if err != nil {
			t.Fatal(err)
		}
		return contacttest.Fixture{
			Repository: repo,
			ReadAll: func(t *testing.T) []models.ContactSubmission {
				t.Helper()
				rows, err := db.QueryContext(t.Context(), `SELECT message, created_at, email, id, name FROM hyperserver_contact_submission`)
				if err != nil {
					t.Fatal(err)
				}
				defer rows.Close()
				var records []models.ContactSubmission
				for rows.Next() {
					var record models.ContactSubmission
					if err := rows.Scan(&record.Message, &record.CreatedAt, &record.Email, &record.Id, &record.Name); err != nil {
						t.Fatal(err)
					}
					records = append(records, record)
				}
				if err := rows.Err(); err != nil {
					t.Fatal(err)
				}
				return records
			},
			SetWriteFailure: func(t *testing.T, fail bool) {
				t.Helper()
				query := `DROP TRIGGER fail_contact`
				if fail {
					// Fail after insertion to check that SQLite rolls back the row too.
					query = `CREATE TRIGGER fail_contact AFTER INSERT ON hyperserver_contact_submission
						BEGIN SELECT RAISE(ABORT, 'injected contact storage failure'); END`
				}
				if _, err := db.ExecContext(t.Context(), query); err != nil {
					t.Fatal(err)
				}
			},
		}
	})
}

func TestSQLiteContactCreationTime(t *testing.T) {
	repo, err := NewSQLiteContactRepository(t.Context(), contactTestDB(t))
	if err != nil {
		t.Fatal(err)
	}
	submission := &models.ContactSubmission{Email: "created@example.invalid", CreatedAt: time.Unix(1, 0)}
	started := time.Now()
	if err := repo.Create(t.Context(), submission); err != nil {
		t.Fatal(err)
	}
	if submission.CreatedAt.Before(started) || submission.CreatedAt.After(time.Now()) {
		t.Fatalf("incorrect creation time: %v", submission.CreatedAt)
	}
}
