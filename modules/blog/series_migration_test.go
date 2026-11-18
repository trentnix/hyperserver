package blog

import (
	"os"
	"testing"
)

func TestSeriesMigrationPreservesPosts(t *testing.T) {
	db, filename := schemaTestDB(t)
	run := blogMigrationCommand(t, filename, "migrations")
	if output, err := run("up", "1"); err != nil {
		t.Fatal(output, err)
	}
	seed, err := os.ReadFile("testdata/migration-content.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), string(seed)); err != nil {
		t.Fatal(err)
	}
	before := blogMigrationSnapshot(t, db, "schema")
	if err := prepareBlogSchema(t.Context(), db); err == nil {
		t.Fatal("startup silently upgraded a version 1 database")
	}
	if output, err := run("up"); err != nil {
		t.Fatal(output, err)
	}
	if err := prepareBlogSchema(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	if got := blogMigrationSnapshot(t, db, "schema", "blog_collections", "blog_collection_content"); got != before {
		t.Fatal("series upgrade changed existing content or other module data")
	}
	if _, err := db.Exec(`INSERT INTO blog_collections(id,title,author_id) VALUES('collection','A series','author');
INSERT INTO blog_collection_content(collection_id,post_id,position) SELECT 'collection',id,0 FROM blog_current_posts LIMIT 1`); err != nil {
		t.Fatal(err)
	}
	if output, err := run("down", "1"); err != nil {
		t.Fatal(output, err)
	}
	if got := blogMigrationSnapshot(t, db, "schema"); got != before {
		t.Fatal("series downgrade changed posts, history, media, or other module data")
	}
	if output, err := run("up"); err != nil {
		t.Fatal(output, err)
	}
	if err := prepareBlogSchema(t.Context(), db); err != nil {
		t.Fatal(err)
	}
}
