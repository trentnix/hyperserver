package storage

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/modules/site/models"
)

const contactSchemaFixture = `CREATE TABLE hyperserver_contact_submission (
	id VARCHAR(36) PRIMARY KEY,
	email VARCHAR(255) NOT NULL,
	name TEXT,
	message TEXT,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
)`

// Version metadata keeps declaration tests focused on schema compatibility.
func versionedContactTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db := contactTestDB(t)
	if _, err := db.ExecContext(t.Context(), `CREATE TABLE hyperserver_contact_version (version uint64, dirty bool);
		INSERT INTO hyperserver_contact_version VALUES (1, false)`); err != nil {
		t.Fatal(err)
	}
	return db
}

func requireContactSchemaError(t *testing.T, db *sql.DB, want string) {
	t.Helper()
	var before string
	if err := db.QueryRowContext(t.Context(), "SELECT sql FROM sqlite_master WHERE name = 'hyperserver_contact_submission'").Scan(&before); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		repository, err := NewSQLiteContactRepository(t.Context(), db)
		if repository != nil || err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("incompatible schema returned repository=%v, error=%v, want %q", repository, err, want)
		}
	}
	var after string
	if err := db.QueryRowContext(t.Context(), "SELECT sql FROM sqlite_master WHERE name = 'hyperserver_contact_submission'").Scan(&after); err != nil || after != before {
		t.Fatalf("startup changed the existing schema or closed the borrowed pool: %q, %v", after, err)
	}
}

func TestSQLiteContactColumnDeclarations(t *testing.T) {
	for _, column := range []struct{ name, declaredType string }{
		{"id", "VARCHAR(36)"},
		{"email", "VARCHAR(255)"},
		{"name", "TEXT"},
		{"message", "TEXT"},
		{"created_at", "DATETIME"},
	} {
		t.Run(column.name+"/missing", func(t *testing.T) {
			db := versionedContactTestDB(t)
			schema := strings.Replace(contactSchemaFixture, "\n\t"+column.name+" ", "\n\tlegacy_column ", 1)
			if _, err := db.ExecContext(t.Context(), schema); err != nil {
				t.Fatal(err)
			}
			requireContactSchemaError(t, db, "missing "+column.name)
			if _, err := db.ExecContext(t.Context(), "ALTER TABLE hyperserver_contact_submission RENAME COLUMN legacy_column TO "+column.name); err != nil {
				t.Fatal(err)
			}
			if _, err := NewSQLiteContactRepository(t.Context(), db); err != nil {
				t.Fatal("retry after explicit repair:", err)
			}
		})
		types := []string{"", "BLOB", "INTEGER", "CHARINT", "NUMERIC"}
		if column.name == "created_at" {
			types = append(types, "TEXT", "DATETIME(6)", `" DATETIME "`)
		}
		for _, declaredType := range types {
			t.Run(column.name+"/type/"+declaredType, func(t *testing.T) {
				db := versionedContactTestDB(t)
				schema := strings.Replace(contactSchemaFixture, column.name+" "+column.declaredType, column.name+" "+declaredType, 1)
				if _, err := db.ExecContext(t.Context(), schema); err != nil {
					t.Fatal(err)
				}
				requireContactSchemaError(t, db, column.name+" requires a supported")
			})
		}
		for _, storage := range []string{"VIRTUAL", "STORED"} {
			t.Run(column.name+"/generated/"+storage, func(t *testing.T) {
				db := versionedContactTestDB(t)
				schema := strings.Replace(contactSchemaFixture, "PRIMARY KEY", "UNIQUE", 1)
				declaration := column.name + " " + column.declaredType
				schema = strings.Replace(schema, declaration+" DEFAULT CURRENT_TIMESTAMP", declaration, 1)
				schema = strings.Replace(schema, declaration, declaration+" GENERATED ALWAYS AS ('fixed') "+storage, 1)
				if _, err := db.ExecContext(t.Context(), schema); err != nil {
					t.Fatal(err)
				}
				requireContactSchemaError(t, db, column.name+" must be an ordinary writable column")
			})
		}
	}
}

func TestSQLiteContactRequiresNonNullEmail(t *testing.T) {
	db := versionedContactTestDB(t)
	schema := strings.Replace(contactSchemaFixture, "email VARCHAR(255) NOT NULL", "email VARCHAR(255)", 1)
	if _, err := db.ExecContext(t.Context(), schema+`; INSERT INTO hyperserver_contact_submission (id, email, message) VALUES ('existing', NULL, 'preserved')`); err != nil {
		t.Fatal(err)
	}
	requireContactSchemaError(t, db, "email requires NOT NULL")
	var count int
	if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM hyperserver_contact_submission WHERE id = 'existing' AND email IS NULL AND message = 'preserved'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("startup changed invalid data: %d, %v", count, err)
	}
}

func TestSQLiteContactUniqueID(t *testing.T) {
	for _, tc := range []struct {
		name, key, index string
		wantError        bool
	}{
		{name: "primary key", key: "PRIMARY KEY"},
		{name: "unique constraint", key: "UNIQUE"},
		{name: "explicit index", index: `CREATE UNIQUE INDEX "contact ID" ON hyperserver_contact_submission(ID DESC)`},
		{name: "missing", wantError: true},
		{name: "ordinary index", index: "CREATE INDEX contact_id ON hyperserver_contact_submission(id)", wantError: true},
		{name: "partial", index: "CREATE UNIQUE INDEX contact_id ON hyperserver_contact_submission(id) WHERE email IS NOT NULL", wantError: true},
		{name: "composite", index: "CREATE UNIQUE INDEX contact_id ON hyperserver_contact_submission(id, email)", wantError: true},
		{name: "expression", index: "CREATE UNIQUE INDEX contact_id ON hyperserver_contact_submission(lower(id))", wantError: true},
		{name: "extra expression", index: "CREATE UNIQUE INDEX contact_id ON hyperserver_contact_submission(id, lower(id))", wantError: true},
		{name: "nonbinary", index: "CREATE UNIQUE INDEX contact_id ON hyperserver_contact_submission(id COLLATE NOCASE)", wantError: true},
		{name: "wrong column", index: "CREATE UNIQUE INDEX contact_id ON hyperserver_contact_submission(email)", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := versionedContactTestDB(t)
			schema := strings.Replace(contactSchemaFixture, "PRIMARY KEY", tc.key, 1)
			if _, err := db.ExecContext(t.Context(), schema); err != nil {
				t.Fatal(err)
			}
			if tc.index != "" {
				if _, err := db.ExecContext(t.Context(), tc.index); err != nil {
					t.Fatal(err)
				}
			}
			if tc.wantError {
				requireContactSchemaError(t, db, "id requires a non-partial unique key with BINARY comparison")
				return
			}
			repository, err := NewSQLiteContactRepository(t.Context(), db)
			if err != nil {
				t.Fatal("compatible unique key rejected:", err)
			}
			submission := models.ContactSubmission{Email: "person@example.invalid", Message: "preserved"}
			if err := repository.Create(t.Context(), &submission); err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(t.Context(), "INSERT INTO hyperserver_contact_submission SELECT * FROM hyperserver_contact_submission"); err == nil {
				t.Fatal("duplicate ID accepted")
			}
			var count int
			if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM hyperserver_contact_submission WHERE id = ? AND message = 'preserved'", submission.Id).Scan(&count); err != nil || count != 1 {
				t.Fatalf("failed insert changed stored data: %d, %v", count, err)
			}
		})
	}
}

func TestSQLiteCompatibleContactSchemaRoundTrip(t *testing.T) {
	for _, timestamp := range []string{"DATE", "datetime", "TIMESTAMP"} {
		for _, suffix := range []string{"", " WITHOUT ROWID"} {
			t.Run(timestamp+suffix, func(t *testing.T) {
				db := versionedContactTestDB(t)
				schema := `CREATE TABLE hyperserver_contact_submission (
					extra TEXT, MESSAGE CLOB NOT NULL, NAME NCHAR(100) NOT NULL,
					ID TEXT NOT NULL PRIMARY KEY, EMAIL TEXT NOT NULL, CREATED_AT ` + timestamp + ` NOT NULL
				)` + suffix
				if _, err := db.ExecContext(t.Context(), schema); err != nil {
					t.Fatal(err)
				}
				repository, err := NewSQLiteContactRepository(t.Context(), db)
				if err != nil {
					t.Fatal("compatible schema rejected:", err)
				}
				submission := models.ContactSubmission{Email: "person@example.invalid", Name: "O'Neil", Message: "<Hello>"}
				if err := repository.Create(t.Context(), &submission); err != nil {
					t.Fatal(err)
				}
				if _, err := NewSQLiteContactRepository(t.Context(), db); err != nil {
					t.Fatal("repeated initialization:", err)
				}
				var stored models.ContactSubmission
				if err := db.QueryRowContext(t.Context(), "SELECT id, email, name, message, created_at FROM hyperserver_contact_submission").
					Scan(&stored.Id, &stored.Email, &stored.Name, &stored.Message, &stored.CreatedAt); err != nil {
					t.Fatal(err)
				}
				if !stored.CreatedAt.Equal(submission.CreatedAt) {
					t.Fatal("creation time changed")
				}
				submission.CreatedAt = stored.CreatedAt
				if stored != submission {
					t.Fatalf("stored submission = %+v, want %+v", stored, submission)
				}
			})
		}
	}
}

func TestSQLiteContactSchemaFailurePreservesDuplicates(t *testing.T) {
	db := versionedContactTestDB(t)
	schema := strings.Replace(contactSchemaFixture, "PRIMARY KEY", "", 1)
	if _, err := db.ExecContext(t.Context(), schema+`;
		INSERT INTO hyperserver_contact_submission (id, email, message) VALUES
		('duplicate', 'person@example.invalid', 'first'), ('duplicate', 'person@example.invalid', 'second')`); err != nil {
		t.Fatal(err)
	}
	requireContactSchemaError(t, db, "id requires a non-partial unique key with BINARY comparison")
	var count int
	if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM hyperserver_contact_submission WHERE id = 'duplicate' AND message IN ('first', 'second')").Scan(&count); err != nil || count != 2 {
		t.Fatalf("startup changed duplicate records: %d, %v", count, err)
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE hyperserver_contact_submission SET id = 'distinct' WHERE message = 'second';
		CREATE UNIQUE INDEX contact_id ON hyperserver_contact_submission(id)`); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSQLiteContactRepository(t.Context(), db); err != nil {
		t.Fatal("retry after explicit repair:", err)
	}
	if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM hyperserver_contact_submission WHERE message IN ('first', 'second')").Scan(&count); err != nil || count != 2 {
		t.Fatalf("startup changed repaired records: %d, %v", count, err)
	}
}
