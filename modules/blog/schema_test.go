package blog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mattn/go-sqlite3"
	dbsqlite "github.com/trentnix/hyperserver/pkg/database/sqlite"
)

// TestBlogSchemaBootstrapRollback fails after tables have been created, proving
// schema setup cannot leave a partially initialized database behind.
func TestBlogSchemaBootstrapRollback(t *testing.T) {
	db, _ := schemaTestDB(t)
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Raw(func(driver any) error {
		driver.(*sqlite3.SQLiteConn).RegisterAuthorizer(func(op int, name, _, _ string) int {
			if op == sqlite3.SQLITE_CREATE_INDEX && name == "blog_pages_parent" {
				return sqlite3.SQLITE_DENY
			}
			return sqlite3.SQLITE_OK
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	conn.Close()
	before := schemaSnapshot(t, db)
	if err := prepareBlogSchema(t.Context(), db); err == nil {
		t.Fatal("startup ignored denied index creation")
	}
	if schemaSnapshot(t, db) != before {
		t.Fatal("failed bootstrap left partial tables or metadata")
	}
}

func TestBlogSchemaDeclarations(t *testing.T) {
	for _, change := range [][2]string{
		{"slug TEXT NOT NULL UNIQUE", "slug TEXT NOT NULL"},
		{"draft INTEGER NOT NULL", "draft TEXT NOT NULL"},
		{"title TEXT NOT NULL", "title TEXT"},
		{"REFERENCES blog_posts(id) ON DELETE CASCADE", "REFERENCES blog_posts(id) ON DELETE RESTRICT"},
		{"ON blog_current_pages(parent_id)", "ON blog_current_pages(slug)"},
		{"version INTEGER NOT NULL DEFAULT 1", "version INTEGER NOT NULL DEFAULT 0"},
		{"slug TEXT NOT NULL UNIQUE", "slug TEXT NOT NULL UNIQUE COLLATE NOCASE"},
		{"UNIQUE (owner_id, id)", "UNIQUE (id)"},
		{"REFERENCES blog_posts(owner_id, id)", "REFERENCES blog_posts(id, owner_id)"},
		{"ON blog_post_images(url, version_id);", "ON blog_post_images(url, version_id) WHERE url<>'';"},
		{"id INTEGER PRIMARY KEY AUTOINCREMENT,", "id INTEGER PRIMARY KEY DESC,"},
	} {
		t.Run(change[1], func(t *testing.T) {
			db, _ := schemaTestDB(t)
			schema := strings.ReplaceAll(blogSchema, change[0], change[1])
			if schema == blogSchema {
				t.Fatal("test did not alter schema")
			}
			if _, err := db.Exec(schema+blogVersionSchema+`; INSERT INTO blog_schema_version VALUES(?,0)`, requiredBlogSchemaVersion); err != nil {
				t.Fatal(err)
			}
			before := schemaSnapshot(t, db)
			if err := prepareBlogSchema(t.Context(), db); err == nil {
				t.Fatal("accepted incompatible schema")
			}
			if schemaSnapshot(t, db) != before {
				t.Fatal("validation changed storage")
			}
		})
	}
}

func TestBlogSchemaExtraIndexes(t *testing.T) {
	db, _ := schemaTestDB(t)
	if err := prepareBlogSchema(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE INDEX operator_post_date ON blog_current_posts(published DESC) WHERE draft=0;
CREATE INDEX operator_lower_title ON blog_pages(lower(title));
CREATE INDEX blog_operator_tag ON blog_media(tags);`); err != nil {
		t.Fatal(err)
	}
	before := schemaSnapshot(t, db)
	if err := prepareBlogSchema(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	if schemaSnapshot(t, db) != before {
		t.Fatal("validation changed operator indexes")
	}
}

func TestBlogSchemaEquivalentKeyOrder(t *testing.T) {
	db, _ := schemaTestDB(t)
	schema := strings.Replace(blogSchema,
		"id TEXT PRIMARY KEY, version_id INTEGER NOT NULL,\n slug TEXT NOT NULL UNIQUE",
		"slug TEXT NOT NULL UNIQUE, version_id INTEGER NOT NULL,\n id TEXT PRIMARY KEY", 1)
	if schema == blogSchema {
		t.Fatal("test did not reorder keys")
	}
	if _, err := db.Exec(schema+blogVersionSchema+`; INSERT INTO blog_schema_version VALUES(?,0)`, requiredBlogSchemaVersion); err != nil {
		t.Fatal(err)
	}
	before := schemaSnapshot(t, db)
	if err := prepareBlogSchema(t.Context(), db); err != nil {
		t.Fatal("rejected equivalent keys with different generated index names", err)
	}
	if schemaSnapshot(t, db) != before {
		t.Fatal("validation changed storage")
	}
}

// Equivalent declarations can differ after SQLite rewrites them during a
// migration. Validation must inspect structure without changing stored content.
func TestBlogSchemaEquivalentDeclarations(t *testing.T) {
	for _, change := range []struct{ name, sql string }{
		{"column round trip", `ALTER TABLE blog_media DROP COLUMN tags;
ALTER TABLE blog_media ADD COLUMN tags TEXT NOT NULL DEFAULT ''`},
		{"column order and quoting", `ALTER TABLE blog_media RENAME TO previous_media;
CREATE TABLE "blog_media" (tags TEXT NOT NULL DEFAULT '', version INTEGER NOT NULL CHECK(version>0), label TEXT NOT NULL, filename TEXT NOT NULL, id TEXT PRIMARY KEY);
INSERT INTO blog_media(id,filename,label,version,tags) SELECT id,filename,label,version,tags FROM previous_media;
DROP TABLE previous_media`},
	} {
		t.Run(change.name, func(t *testing.T) {
			db, _ := schemaTestDB(t)
			if err := prepareBlogSchema(t.Context(), db); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO blog_media(id,filename,label,version) VALUES('image','image.png','Example',1);` + change.sql); err != nil {
				t.Fatal(err)
			}
			before := schemaSnapshot(t, db)
			if err := prepareBlogSchema(t.Context(), db); err != nil {
				t.Fatal("rejected equivalent schema", err)
			}
			if schemaSnapshot(t, db) != before {
				t.Fatal("validation changed storage")
			}
		})
	}
}

func TestBlogSchemaSpecificErrors(t *testing.T) {
	for _, test := range []struct{ change, want string }{
		{`DROP TABLE blog_posts`, "missing table blog_posts"},
		{`DROP VIEW blog_post_current; ALTER TABLE blog_posts DROP COLUMN tags`, "table blog_posts columns"},
		{`DROP INDEX blog_post_images_url`, "index blog_post_images_url"},
		{`DROP TRIGGER blog_pages_no_cycle`, "missing trigger blog_pages_no_cycle"},
		{`CREATE TABLE blog_projects(id TEXT)`, "unsupported table blog_projects"},
	} {
		t.Run(test.change, func(t *testing.T) {
			db, _ := schemaTestDB(t)
			if err := prepareBlogSchema(t.Context(), db); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(test.change); err != nil {
				t.Fatal(err)
			}
			before := schemaSnapshot(t, db)
			if err := prepareBlogSchema(t.Context(), db); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("want error containing %q, got %v", test.want, err)
			}
			if schemaSnapshot(t, db) != before {
				t.Fatal("rejected validation changed storage")
			}
		})
	}
}

// schemaTestDB opens an isolated file without initializing any module.
func schemaTestDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	name := filepath.Join(t.TempDir(), "blog.db")
	db, err := dbsqlite.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, name
}

// schemaSnapshot records declarations and rows, including other modules' data.
func schemaSnapshot(t *testing.T, db *sql.DB) string {
	t.Helper()
	rows, err := db.Query(`SELECT type, name, sql FROM sqlite_master ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var objects [][3]string
	var tables []string
	for rows.Next() {
		var kind, name string
		var ddl sql.NullString
		if err := rows.Scan(&kind, &name, &ddl); err != nil {
			t.Fatal(err)
		}
		objects = append(objects, [3]string{kind, name, ddl.String})
		if kind == "table" {
			tables = append(tables, name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	data := map[string]any{"schema": objects}
	for _, table := range tables {
		rows, err := db.Query(`SELECT * FROM "` + strings.ReplaceAll(table, `"`, `""`) + `" ORDER BY rowid`)
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		var records [][]any
		for rows.Next() {
			values, targets := make([]any, len(columns)), make([]any, len(columns))
			for i := range values {
				targets[i] = &values[i]
			}
			if err := rows.Scan(targets...); err != nil {
				t.Fatal(err)
			}
			records = append(records, values)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		data[table] = records
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func TestBlogSchemaBootstrap(t *testing.T) {
	db, _ := schemaTestDB(t)
	if _, err := db.Exec(`CREATE TABLE other_version(version INTEGER); INSERT INTO other_version VALUES(73)`); err != nil {
		t.Fatal(err)
	}
	if err := prepareBlogSchema(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	var version int
	var dirty bool
	if err := db.QueryRow(`SELECT version, dirty FROM blog_schema_version`).Scan(&version, &dirty); err != nil || version != requiredBlogSchemaVersion || dirty {
		t.Fatal(version, dirty, err)
	}
	before := schemaSnapshot(t, db)
	for range 2 {
		if err := prepareBlogSchema(t.Context(), db); err != nil {
			t.Fatal(err)
		}
	}
	if schemaSnapshot(t, db) != before {
		t.Fatal("startup changed existing storage")
	}
}

func TestBlogSchemaRejection(t *testing.T) {
	for _, change := range []string{
		`DROP TABLE blog_schema_version`,
		`DELETE FROM blog_schema_version`,
		`UPDATE blog_schema_version SET version=0`,
		`UPDATE blog_schema_version SET version=2`,
		`UPDATE blog_schema_version SET version=3`,
		`UPDATE blog_schema_version SET version=-1`,
		`UPDATE blog_schema_version SET dirty=1`,
		`UPDATE blog_schema_version SET dirty=2`,
		`UPDATE blog_schema_version SET version=NULL`,
		`UPDATE blog_schema_version SET dirty=NULL`,
		`UPDATE blog_schema_version SET version='invalid'`,
		`INSERT INTO blog_schema_version VALUES (1, 0)`,
		`ALTER TABLE blog_schema_version RENAME COLUMN dirty TO invalid`,
		`DROP TABLE blog_schema_version; CREATE VIEW blog_schema_version AS SELECT 1 AS version, 0 AS dirty`,
		`DROP VIEW blog_post_current; ALTER TABLE blog_posts DROP COLUMN tags`,
		`DROP VIEW blog_page_current; ALTER TABLE blog_current_pages DROP COLUMN ever_published`,
		`DROP VIEW blog_profile_current; ALTER TABLE blog_current_profile DROP COLUMN version`,
		`ALTER TABLE blog_media DROP COLUMN tags`,
		`DROP TABLE blog_posts`,
		`DROP TABLE blog_pages`,
		`DROP TABLE blog_profile`,
		`DROP TABLE blog_post_images`,
		`DROP TABLE blog_page_images`,
		`DROP INDEX blog_pages_parent`,
		`DROP TRIGGER blog_pages_no_cycle`,
		`CREATE TABLE blog_projects(id TEXT)`,
	} {
		t.Run(change, func(t *testing.T) {
			db, _ := schemaTestDB(t)
			if err := prepareBlogSchema(t.Context(), db); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(change); err != nil {
				t.Fatal(err)
			}
			before := schemaSnapshot(t, db)
			if err := prepareBlogSchema(t.Context(), db); err == nil {
				t.Fatal("startup accepted incompatible storage")
			}
			if schemaSnapshot(t, db) != before {
				t.Fatal("rejected startup changed storage")
			}
		})
	}
}

// Obsolete development databases are rejected, not migrated or relabeled.
func TestBlogSchemaRejectsObsoleteStorage(t *testing.T) {
	db, _ := schemaTestDB(t)
	if _, err := db.Exec(`CREATE TABLE blog_posts(id TEXT PRIMARY KEY, title TEXT);
INSERT INTO blog_posts VALUES('post','Keep');
CREATE TABLE bookmarks(id INTEGER PRIMARY KEY, post_id TEXT REFERENCES blog_posts(id) ON DELETE CASCADE);
INSERT INTO bookmarks VALUES(1,'post');` + blogVersionSchema + `; INSERT INTO blog_schema_version VALUES(1,0)`); err != nil {
		t.Fatal(err)
	}
	before := schemaSnapshot(t, db)
	if err := prepareBlogSchema(t.Context(), db); err == nil {
		t.Fatal("accepted obsolete storage")
	}
	if schemaSnapshot(t, db) != before {
		t.Fatal("startup changed obsolete content or another module's records")
	}
}

func TestBlogSchemaCancellation(t *testing.T) {
	db, _ := schemaTestDB(t)
	before := schemaSnapshot(t, db)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := prepareBlogSchema(ctx, db); !errors.Is(err, context.Canceled) {
		t.Fatal("ignored cancellation", err)
	}
	if schemaSnapshot(t, db) != before {
		t.Fatal("canceled setup changed storage")
	}
}
