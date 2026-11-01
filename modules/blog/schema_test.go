package blog

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mattn/go-sqlite3"
	"github.com/trentnix/hyperserver/internal/maintenance"
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
	for _, change := range [][3]string{
		{"slug TEXT NOT NULL UNIQUE", "slug TEXT NOT NULL", "blog_posts requires a non-partial unique key on (slug)"},
		{"slug TEXT NOT NULL UNIQUE", "slug TEXT NOT NULL UNIQUE COLLATE NOCASE", "blog_posts requires a non-partial unique key on (slug)"},
		{"draft INTEGER NOT NULL", "draft TEXT NOT NULL", "blog_posts.draft"},
		{"title TEXT NOT NULL", "title TEXT", "blog_posts.title"},
		{"REFERENCES blog_posts(id) ON DELETE CASCADE", "REFERENCES blog_posts(id) ON DELETE RESTRICT", "blog_post_images.owner_id"},
		{"CREATE INDEX blog_pages_parent ON blog_pages(parent_id)", "CREATE INDEX blog_pages_parent ON blog_pages(title)", "index blog_pages_parent"},
		{"CREATE INDEX blog_pages_parent ON blog_pages(parent_id)", "CREATE INDEX blog_pages_parent ON blog_pages(parent_id) WHERE parent_id IS NOT NULL", "index blog_pages_parent"},
		{"ON blog_page_revisions(owner_id, id)", "ON blog_page_revisions(id, owner_id)", "index blog_page_revisions_owner"},
		{"BEFORE UPDATE OF parent_id", "BEFORE UPDATE OF title", "trigger blog_pages_no_cycle"},
		{"id INTEGER PRIMARY KEY AUTOINCREMENT", "id INTEGER", "blog_post_revisions.id"},
		{"id INTEGER PRIMARY KEY AUTOINCREMENT", "id INTEGER PRIMARY KEY DESC", "blog_post_revisions.id requires an INTEGER PRIMARY KEY"},
		{"version INTEGER NOT NULL DEFAULT 1", "version INTEGER NOT NULL DEFAULT 0", "blog_pages.version requires DEFAULT 1"},
		{"tags TEXT NOT NULL DEFAULT ''", "tags TEXT NOT NULL", "blog_media.tags requires DEFAULT ''"},
		{"tags TEXT NOT NULL DEFAULT ''", "tags TEXT GENERATED ALWAYS AS ('') VIRTUAL NOT NULL", "blog_posts.tags"},
	} {
		t.Run(change[1], func(t *testing.T) {
			db, _ := schemaTestDB(t)
			if _, err := db.Exec(strings.ReplaceAll(blogSchemaV1, change[0], change[1])); err != nil {
				t.Fatal(err)
			}
			before := schemaSnapshot(t, db)
			if err := adoptBlogSchema(t.Context(), db); err == nil || !strings.Contains(err.Error(), change[2]) {
				t.Fatalf("want error identifying %q, got %v", change[2], err)
			}
			if schemaSnapshot(t, db) != before {
				t.Fatal("rejected adoption changed storage")
			}
		})
	}
}

// TestBlogSchemaExtraIndexes allows operator-added indexes during both adoption
// and startup, without modifying them or weakening required uniqueness checks.
func TestBlogSchemaExtraIndexes(t *testing.T) {
	for _, versioned := range []bool{false, true} {
		t.Run(fmt.Sprint(versioned), func(t *testing.T) {
			db, _ := schemaTestDB(t)
			if _, err := db.Exec(unversionedBlogSchema); err != nil {
				t.Fatal(err)
			}
			if versioned {
				if err := adoptBlogSchema(t.Context(), db); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.Exec(`CREATE INDEX blog_posts_title ON blog_posts(title);
CREATE INDEX operator_post_date ON blog_posts(published DESC) WHERE draft=0;
CREATE INDEX operator_lower_title ON blog_pages(lower(title));`); err != nil {
				t.Fatal(err)
			}
			before := schemaSnapshot(t, db)
			if err := adoptBlogSchema(t.Context(), db); err != nil {
				t.Fatal(err)
			}
			if err := prepareBlogSchema(t.Context(), db); err != nil {
				t.Fatal(err)
			}
			if !versioned {
				if _, err := db.Exec(`DROP TABLE blog_schema_version`); err != nil {
					t.Fatal(err)
				}
			}
			if schemaSnapshot(t, db) != before {
				t.Fatal("validation changed existing indexes or data")
			}
		})
	}
}

func TestBlogSchemaSpecificErrors(t *testing.T) {
	for _, test := range []struct{ change, want string }{
		{`DROP TABLE blog_post_revisions`, "missing table blog_post_revisions"},
		{`ALTER TABLE blog_posts DROP COLUMN tags`, "missing column blog_posts.tags"},
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
			for _, operation := range []func(context.Context, *sql.DB) error{prepareBlogSchema, adoptBlogSchema} {
				if err := operation(t.Context(), db); err == nil || !strings.Contains(err.Error(), test.want) {
					t.Fatalf("want error containing %q, got %v", test.want, err)
				}
			}
			if schemaSnapshot(t, db) != before {
				t.Fatal("rejected validation changed storage")
			}
		})
	}
}

func TestBlogSchemaUniqueKeys(t *testing.T) {
	for _, test := range []struct {
		index string
		valid bool
	}{
		{`CREATE UNIQUE INDEX custom_slug ON blog_posts(slug)`, true},
		{`CREATE INDEX custom_slug ON blog_posts(slug)`, false},
		{`CREATE UNIQUE INDEX custom_slug ON blog_posts(slug) WHERE draft=0`, false},
		{`CREATE UNIQUE INDEX custom_slug ON blog_posts(lower(slug))`, false},
		{`CREATE UNIQUE INDEX custom_slug ON blog_posts(slug, title)`, false},
		{`CREATE UNIQUE INDEX custom_slug ON blog_posts(slug COLLATE NOCASE)`, false},
	} {
		t.Run(test.index, func(t *testing.T) {
			db, _ := schemaTestDB(t)
			schema := strings.Replace(blogSchemaV1, "slug TEXT NOT NULL UNIQUE", "slug TEXT NOT NULL", 1)
			if _, err := db.Exec(schema + test.index); err != nil {
				t.Fatal(err)
			}
			err := adoptBlogSchema(t.Context(), db)
			if test.valid && err != nil {
				t.Fatal(err)
			}
			if !test.valid && (err == nil || !strings.Contains(err.Error(), "blog_posts requires a non-partial unique key on (slug)")) {
				t.Fatalf("want error naming slug uniqueness, got %v", err)
			}
		})
	}
}

func TestBlogSchemaAdoptionBrokenReferences(t *testing.T) {
	db, _ := schemaTestDB(t)
	if _, err := db.Exec(blogSchemaV1 + `
PRAGMA foreign_keys=OFF;
INSERT INTO blog_post_images(owner_id,url) VALUES('missing','/images/private.png');
PRAGMA foreign_keys=ON;`); err != nil {
		t.Fatal(err)
	}
	before := schemaSnapshot(t, db)
	if err := adoptBlogSchema(t.Context(), db); err == nil || !strings.Contains(err.Error(), "foreign-key") {
		t.Fatal("adopted broken references", err)
	}
	if schemaSnapshot(t, db) != before {
		t.Fatal("failed reference check changed storage")
	}
}

//go:embed testdata/schema-unversioned.sql
var unversionedBlogSchema string

// TestBlogSchemaLegacyFixture tests the pre-migration release independently of
// the current bootstrap SQL, including the real offline configuration path.
func TestBlogSchemaLegacyFixture(t *testing.T) {
	db, name := schemaTestDB(t)
	if _, err := db.Exec(unversionedBlogSchema + `
INSERT INTO blog_posts(id,slug,title,summary,markdown,published,draft,version,tags)
VALUES('legacy','legacy','Original','Summary','Body',1700000000,0,7,'old');
INSERT INTO blog_post_revisions(owner_id,title,summary,markdown,preview_image,recorded,tags)
VALUES('legacy','Original','Summary','Body','',42,'old');`); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Dir(name)
	if err := os.MkdirAll(filepath.Join(directory, "modules", "blog"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "modules", "blog", "config.yaml"), []byte("storage:\n  provider: sqlite\n  connection: blog.db\n"), 0600); err != nil {
		t.Fatal(err)
	}
	before := schemaSnapshot(t, db)
	lock, err := maintenance.Lock(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := AdoptSchema(t.Context(), directory); err == nil {
		t.Fatal("adoption ignored the running server's lock")
	}
	lock.Close()
	if err := AdoptSchema(t.Context(), directory); err != nil {
		t.Fatal(err)
	}
	if err := prepareBlogSchema(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE blog_schema_version`); err != nil {
		t.Fatal(err)
	}
	if schemaSnapshot(t, db) != before {
		t.Fatal("adoption changed legacy content or history")
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
	if err := db.QueryRow(`SELECT version, dirty FROM blog_schema_version`).Scan(&version, &dirty); err != nil || version != 1 || dirty {
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
		`UPDATE blog_schema_version SET version=-1`,
		`UPDATE blog_schema_version SET dirty=1`,
		`UPDATE blog_schema_version SET dirty=2`,
		`UPDATE blog_schema_version SET version=NULL`,
		`UPDATE blog_schema_version SET dirty=NULL`,
		`UPDATE blog_schema_version SET version='invalid'`,
		`INSERT INTO blog_schema_version VALUES (1, 0)`,
		`ALTER TABLE blog_schema_version RENAME COLUMN dirty TO invalid`,
		`DROP TABLE blog_schema_version; CREATE VIEW blog_schema_version AS SELECT 1 AS version, 0 AS dirty`,
		`ALTER TABLE blog_posts DROP COLUMN tags`,
		`ALTER TABLE blog_pages DROP COLUMN ever_published`,
		`ALTER TABLE blog_profile DROP COLUMN version`,
		`ALTER TABLE blog_media DROP COLUMN tags`,
		`DROP TABLE blog_post_revisions`,
		`DROP TABLE blog_page_revisions`,
		`DROP TABLE blog_profile_revisions`,
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

// TestBlogSchemaAdoption preserves private content, publication state, edit tokens,
// history, media references, profile settings, and setup progress across adoption.
func TestBlogSchemaAdoption(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	s := h.blog.posts.(*sqliteRepository)
	for _, draft := range []bool{false, true} {
		p, err := savePostFixture(s, t.Context(), Post{Slug: fmt.Sprint("post-", draft), Title: fmt.Sprint("Post ", draft), Markdown: "![Image](/images/example.png)", Draft: draft, Tags: "tag"})
		if err != nil {
			t.Fatal(err)
		}
		p.Title += " edited"
		if _, err := savePostFixture(s, t.Context(), p); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SaveMediaMetadata(t.Context(), "example.png", mediaMetadata{Filename: "original.png", Label: "Example", Tags: "example"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DROP TABLE blog_schema_version; CREATE TABLE other_version(version INTEGER); INSERT INTO other_version VALUES(73)`); err != nil {
		t.Fatal(err)
	}
	before := schemaSnapshot(t, s.db)
	if err := prepareBlogSchema(t.Context(), s.db); err == nil || !strings.Contains(err.Error(), "adopt-schema") {
		t.Fatal("startup must explain adoption", err)
	}
	for range 2 {
		if err := adoptBlogSchema(t.Context(), s.db); err != nil {
			t.Fatal(err)
		}
		if err := prepareBlogSchema(t.Context(), s.db); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec(`DROP TABLE blog_schema_version`); err != nil {
		t.Fatal(err)
	}
	if schemaSnapshot(t, s.db) != before {
		t.Fatal("adoption changed more than the version marker")
	}
}

func TestBlogSchemaAdoptionRejection(t *testing.T) {
	for _, change := range []string{
		`DROP TABLE blog_posts`,
		`ALTER TABLE blog_posts DROP COLUMN draft`,
		`ALTER TABLE blog_posts DROP COLUMN preview_image`,
		`ALTER TABLE blog_posts DROP COLUMN version`,
		`ALTER TABLE blog_post_revisions DROP COLUMN tags`,
		`DROP INDEX blog_post_images_url`,
		`DROP INDEX blog_page_revisions_owner`,
		`CREATE VIEW blog_schema_version AS SELECT 1 AS version, 0 AS dirty`,
		blogVersionSchema + `; INSERT INTO blog_schema_version VALUES(2,0)`,
		blogVersionSchema + `; INSERT INTO blog_schema_version VALUES(1,1)`,
		blogVersionSchema,
		`CREATE TABLE blog_legacy(id INTEGER)`,
	} {
		t.Run(change, func(t *testing.T) {
			db, _ := schemaTestDB(t)
			if _, err := db.Exec(blogSchemaV1 + change); err != nil {
				t.Fatal(err)
			}
			before := schemaSnapshot(t, db)
			if err := adoptBlogSchema(t.Context(), db); err == nil {
				t.Fatal("adopted an unsupported schema")
			}
			if schemaSnapshot(t, db) != before {
				t.Fatal("failed adoption changed storage")
			}
		})
	}
}

func TestBlogSchemaCancellation(t *testing.T) {
	db, _ := schemaTestDB(t)
	if _, err := db.Exec(blogSchemaV1); err != nil {
		t.Fatal(err)
	}
	before := schemaSnapshot(t, db)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, operation := range []func(context.Context, *sql.DB) error{prepareBlogSchema, adoptBlogSchema} {
		if err := operation(ctx, db); !errors.Is(err, context.Canceled) {
			t.Fatal("ignored cancellation", err)
		}
	}
	if schemaSnapshot(t, db) != before {
		t.Fatal("canceled setup changed storage")
	}
}

func TestBlogSchemaAdoptionMissingFile(t *testing.T) {
	directory := t.TempDir()
	if err := os.MkdirAll(filepath.Join(directory, "modules", "blog"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "modules", "blog", "config.yaml"), []byte("storage:\n  provider: sqlite\n  connection: missing.db\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := AdoptSchema(t.Context(), directory); err == nil {
		t.Fatal("adopted a missing database")
	}
	if _, err := os.Stat(filepath.Join(directory, "missing.db")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("adoption created the database", err)
	}
}
