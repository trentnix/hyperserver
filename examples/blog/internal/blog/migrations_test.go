package blog

import (
	"context"
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
	for _, source := range []string{"startup", "cli", "adoption"} {
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
			case "adoption":
				if _, err := db.Exec(unversionedBlogSchema); err != nil {
					t.Fatal(err)
				}
				if err := adoptBlogSchema(t.Context(), db); err != nil {
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
			if _, err := db.Exec(`INSERT INTO blog_pages(id,slug,title,markdown,published,parent_id,summary,preview_image,children_display,updated)
VALUES('parent','parent','Parent','',1,NULL,'','','hidden',1),('child','child','Child','',1,'parent','','','hidden',1)`); err != nil {
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
	directory := t.TempDir()
	run := blogMigrationCommand(t, filename, directory)
	for _, name := range []string{"000001_blog.up.sql", "000001_blog.down.sql"} {
		data, err := os.ReadFile(filepath.Join("migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
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
