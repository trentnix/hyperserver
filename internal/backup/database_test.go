package backup

import (
	"os"
	"path/filepath"
	"testing"

	dbsqlite "github.com/trentnix/hyperserver/pkg/database/sqlite"
)

func TestDatabaseChecks(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source.db")
	pool, err := dbsqlite.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec("CREATE TABLE parent(id INTEGER PRIMARY KEY); CREATE TABLE child(id INTEGER REFERENCES parent(id)); PRAGMA foreign_keys=OFF; INSERT INTO child VALUES(99)"); err != nil {
		t.Fatal(err)
	}
	if err := checkDatabase(t.Context(), pool); err == nil {
		t.Fatal("foreign key violation passed")
	}
	if err := snapshot(t.Context(), source, filepath.Join(directory, "snapshot.db")); err == nil {
		t.Fatal("invalid database backed up")
	}
	missing := filepath.Join(directory, "missing.db")
	if err := snapshot(t.Context(), missing, filepath.Join(directory, "missing-copy.db")); err == nil {
		t.Fatal("missing source accepted")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("missing source was created")
	}
}

func TestDatabasePaths(t *testing.T) {
	base := t.TempDir()
	for _, connection := range []string{"", ":memory:", "file::memory:", "file:test?mode=memory", "file://remote/db", "file:bad%zz", "test?vfs=unix-none", "test?immutable=1", "test?mode=unknown", "test?_journal=WAL&_journal=DELETE"} {
		if _, err := databasePath(connection, base); err == nil {
			t.Fatalf("accepted %q", connection)
		}
	}
	for _, connection := range []string{"file:space%20name.db?_journal=WAL", "space name.db?_journal=WAL"} {
		name, err := databasePath(connection, base)
		if err != nil || name != filepath.Join(base, "space name.db") {
			t.Fatal(name, err)
		}
	}
}
