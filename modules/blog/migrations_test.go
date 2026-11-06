package blog

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// blogMigrationCommand uses the separately installed CLI, just as deployment does.
func blogMigrationCommand(t *testing.T, filename, directory string) func(...string) (string, error) {
	t.Helper()
	binary := os.Getenv("HS_TEST_MIGRATE")
	if binary == "" {
		t.Skip("set HS_TEST_MIGRATE to the pinned migrate executable")
	}
	connection := url.URL{Scheme: "file", Path: filename, RawQuery: url.Values{
		"x-migrations-table": {"blog_schema_version"}, "_foreign_keys": {"on"},
	}.Encode()}
	return func(command ...string) (string, error) {
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		args := append([]string{"-path", directory, "-database", "sqlite3://" + connection.String()}, command...)
		output, err := exec.CommandContext(ctx, binary, args...).CombinedOutput()
		return string(output), err
	}
}

func TestBlogMigrationCompatibility(t *testing.T) {
	for _, source := range []string{"startup", "cli"} {
		t.Run(source, func(t *testing.T) {
			db, filename := schemaTestDB(t)
			run := blogMigrationCommand(t, filename, "migrations")
			if _, err := db.Exec(`CREATE TABLE other_version(version INTEGER); INSERT INTO other_version VALUES(73)`); err != nil {
				t.Fatal(err)
			}
			switch source {
			case "startup":
				if err := prepareBlogSchema(t.Context(), db); err != nil {
					t.Fatal(err)
				}

			}
			for range 2 {
				if output, err := run("up"); err != nil {
					t.Fatal(output, err)
				}
				if err := prepareBlogSchema(t.Context(), db); err != nil {
					t.Fatal(err)
				}
			}
			// Exercise the self-referencing page table during the destructive down.
			tx, err := db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			for _, p := range []Page{{ID: "parent", Slug: "parent", Title: "Parent"}, {ID: "child", Slug: "child", Title: "Child", ParentID: "parent"}} {
				if err := seedCurrentPage(t.Context(), tx, p); err != nil {
					t.Fatal(err)
				}
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			before := schemaSnapshot(t, db)
			if output, err := run("up"); err != nil || schemaSnapshot(t, db) != before {
				t.Fatal("repeated migration changed content", output, err)
			}
			if output, err := run("down", "1"); err != nil {
				t.Fatal(output, err)
			}
			if err := prepareBlogSchema(t.Context(), db); err == nil {
				t.Fatal("startup silently recreated storage after explicit down")
			}
			if output, err := run("up"); err != nil {
				t.Fatal(output, err)
			}
			if err := prepareBlogSchema(t.Context(), db); err != nil {
				t.Fatal(err)
			}
			var other int
			if err := db.QueryRow(`SELECT version FROM other_version`).Scan(&other); err != nil || other != 73 {
				t.Fatal("migration changed another module's version", other, err)
			}
		})
	}
}

func TestBlogMigrationFailureAndFutureVersion(t *testing.T) {
	db, filename := schemaTestDB(t)
	directory := blogMigrationDirectory(t)
	run := blogMigrationCommand(t, filename, directory)
	write := func(name, sql string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(directory, name), []byte(sql), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := prepareBlogSchema(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	// The CLI adds its own metadata index when first opening a database.
	if output, err := run("up"); err != nil {
		t.Fatal(output, err)
	}
	before := schemaSnapshot(t, db)
	write("000002_failure.up.sql", `ALTER TABLE blog_posts ADD COLUMN failed_change TEXT; INSERT INTO missing_table VALUES (1);`)
	write("000002_failure.down.sql", `ALTER TABLE blog_posts DROP COLUMN failed_change;`)
	if output, err := run("up"); err == nil {
		t.Fatal("invalid migration reported success", output)
	}
	if err := prepareBlogSchema(t.Context(), db); err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatal("startup accepted a dirty migration", err)
	}
	var columns int
	if err := db.QueryRow(`SELECT count(*) FROM pragma_table_info('blog_posts') WHERE name='failed_change'`).Scan(&columns); err != nil || columns != 0 {
		t.Fatal("failed migration did not roll back", columns, err)
	}
	// Only repair the marker after proving the failed SQL rolled back.
	if output, err := run("force", "1"); err != nil || schemaSnapshot(t, db) != before {
		t.Fatal("rollback and explicit repair did not preserve the baseline", output, err)
	}
	write("000002_failure.up.sql", `ALTER TABLE blog_posts ADD COLUMN failed_change TEXT;`)
	if output, err := run("up"); err != nil {
		t.Fatal(output, err)
	}
	future := schemaSnapshot(t, db)
	if err := prepareBlogSchema(t.Context(), db); err == nil || !strings.Contains(err.Error(), "version 2") {
		t.Fatal("old binary accepted a newer database", err)
	}
	if schemaSnapshot(t, db) != future {
		t.Fatal("old binary altered a newer database")
	}
	if output, err := run("down", "1"); err != nil {
		t.Fatal(output, err)
	}
	if err := prepareBlogSchema(t.Context(), db); err != nil {
		t.Fatal(err)
	}
}

// TestBlogMigrationPopulatedRoundTrip exercises reversible schema changes on
// populated storage. The baseline's destructive down is tested separately.
func TestBlogMigrationPopulatedRoundTrip(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		failDown bool
	}{
		{"successful down", false},
		{"failed down and repair", true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			db, filename := schemaTestDB(t)
			directory := blogMigrationDirectory(t)
			run := blogMigrationCommand(t, filename, directory)
			if output, err := run("up"); err != nil {
				t.Fatal(output, err)
			}
			seed, err := os.ReadFile("testdata/migration-content.sql")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(t.Context(), string(seed)); err != nil {
				t.Fatal(err)
			}
			baseline := schemaSnapshot(t, db)
			content := blogMigrationSnapshot(t, db, "schema")

			// Index-only changes let us compare every content column unchanged.
			// These synthetic migrations exist only in this test's temporary directory.
			up := `CREATE INDEX blog_posts_test_title ON blog_posts(title);
CREATE INDEX blog_pages_test_title ON blog_pages(title);`
			down := `DROP INDEX blog_posts_test_title; DROP INDEX blog_pages_test_title;`
			if err := os.WriteFile(filepath.Join(directory, "000002_indexes.up.sql"), []byte(up), 0600); err != nil {
				t.Fatal(err)
			}
			downFile := filepath.Join(directory, "000002_indexes.down.sql")
			if err := os.WriteFile(downFile, []byte(down), 0600); err != nil {
				t.Fatal(err)
			}
			if output, err := run("up"); err != nil {
				t.Fatal(output, err)
			}
			var version int
			var dirty bool
			if err := db.QueryRow(`SELECT version, dirty FROM blog_schema_version`).Scan(&version, &dirty); err != nil || version != 2 || dirty {
				t.Fatal("upgrade did not record clean version 2", version, dirty, err)
			}
			if blogMigrationSnapshot(t, db, "schema") != content {
				t.Fatal("upgrade changed stored content")
			}
			var indexes int
			if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='index' AND name IN ('blog_posts_test_title', 'blog_pages_test_title')`).Scan(&indexes); err != nil || indexes != 2 {
				t.Fatal("upgrade did not create both indexes", indexes, err)
			}

			if scenario.failDown {
				beforeFailure := blogMigrationSnapshot(t, db)
				// Fail after changing rows, cascading child deletion, and dropping
				// indexes. All those changes must roll back, not just the last statement.
				failure := `UPDATE blog_posts SET title='Must roll back';
DELETE FROM blog_current_pages WHERE id='child';
UPDATE blog_profile SET bio='Must roll back';
DELETE FROM blog_media;` + down + ` INSERT INTO missing_table VALUES(1);`
				if err := os.WriteFile(downFile, []byte(failure), 0600); err != nil {
					t.Fatal(err)
				}
				if output, err := run("down", "1"); err == nil || !strings.Contains(output, "no such table: missing_table") {
					t.Fatal("down must fail at the injected final statement", output, err)
				}
				if err := db.QueryRow(`SELECT version, dirty FROM blog_schema_version`).Scan(&version, &dirty); err != nil || version != 1 || !dirty {
					t.Fatal("failed down must leave the target version dirty", version, dirty, err)
				}
				if blogMigrationSnapshot(t, db) != beforeFailure {
					t.Fatal("failed down did not roll back all data and schema changes")
				}
				dirtySnapshot := schemaSnapshot(t, db)
				if err := prepareBlogSchema(t.Context(), db); err == nil || !strings.Contains(err.Error(), "dirty") {
					t.Fatal("startup accepted failed down", err)
				}
				if output, err := run("down", "1"); err == nil {
					t.Fatal("migration retried with a dirty marker", output)
				}
				if schemaSnapshot(t, db) != dirtySnapshot {
					t.Fatal("startup or retry altered dirty storage")
				}
				// Only restore version 2's marker after proving its schema and data
				// survived. Force does not repair schema or recover content.
				if err := os.WriteFile(downFile, []byte(down), 0600); err != nil {
					t.Fatal(err)
				}
				if output, err := run("force", "2"); err != nil {
					t.Fatal(output, err)
				}
			}

			if output, err := run("down", "1"); err != nil {
				t.Fatal(output, err)
			}
			if schemaSnapshot(t, db) != baseline {
				t.Fatal("upgrade/downgrade did not preserve the complete version 1 database")
			}
			if err := prepareBlogSchema(t.Context(), db); err != nil {
				t.Fatal("downgraded database cannot start", err)
			}
			var violations int
			if err := db.QueryRow(`SELECT count(*) FROM pragma_foreign_key_check`).Scan(&violations); err != nil || violations != 0 {
				t.Fatal("downgrade left broken references", violations, err)
			}
		})
	}
}

// SQLite can rewrite DDL formatting during migrations without changing its
// meaning. A clean rollback must be accepted without rewriting the database.
func TestBlogMigrationColumnRoundTrip(t *testing.T) {
	db, filename := schemaTestDB(t)
	directory := blogMigrationDirectory(t)
	run := blogMigrationCommand(t, filename, directory)
	if output, err := run("up"); err != nil {
		t.Fatal(output, err)
	}
	if _, err := db.Exec(`INSERT INTO blog_media(id,filename,label,version,tags) VALUES('image','image.png','Example',1,'kept')`); err != nil {
		t.Fatal(err)
	}
	before := blogMigrationSnapshot(t, db, "schema")
	// Recreate a column while preserving its data. Both directions deliberately
	// use ALTER TABLE rather than copying the bootstrap's CREATE TABLE text.
	change := `ALTER TABLE blog_media RENAME COLUMN tags TO previous_tags;
ALTER TABLE blog_media ADD COLUMN tags TEXT NOT NULL DEFAULT '';
UPDATE blog_media SET tags=previous_tags;
ALTER TABLE blog_media DROP COLUMN previous_tags;`
	for _, name := range []string{"000002_column.up.sql", "000002_column.down.sql"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(change), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if output, err := run("up"); err != nil {
		t.Fatal(output, err)
	}
	if output, err := run("down", "1"); err != nil {
		t.Fatal(output, err)
	}
	if blogMigrationSnapshot(t, db, "schema") != before {
		t.Fatal("column round trip changed content")
	}
	if err := prepareBlogSchema(t.Context(), db); err != nil {
		t.Fatal("startup rejected the restored schema", err)
	}
}

// blogMigrationDirectory copies the baseline SQL so tests can add synthetic
// migrations without changing the application's migration history.
func blogMigrationDirectory(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	for _, name := range []string{"000001_blog.up.sql", "000001_blog.down.sql"} {
		data, err := os.ReadFile(filepath.Join("migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return directory
}

// blogMigrationSnapshot excludes version rows, which the CLI updates outside
// the SQL transaction. Callers check version and dirty state separately. Omitting
// "schema" compares all stored rows while allowing an intentional index change.
func blogMigrationSnapshot(t *testing.T, db *sql.DB, omit ...string) string {
	t.Helper()
	var snapshot map[string]json.RawMessage
	if err := json.Unmarshal([]byte(schemaSnapshot(t, db)), &snapshot); err != nil {
		t.Fatal(err)
	}
	delete(snapshot, "blog_schema_version")
	for _, key := range omit {
		delete(snapshot, key)
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
