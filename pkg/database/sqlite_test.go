//go:build cgo

package database

import (
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3" // This test opens SQLite connections.
)

func TestResolvedConnectionSurvivesDirectoryChange(t *testing.T) {
	base := t.TempDir()
	connection, err := ResolveConnection("sqlite3", "file:a%20b.db?mode=rwc", base)
	if err != nil {
		t.Fatal(err)
	}
	db, err := Setup("sqlite3", connection)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxIdleConns(0) // Every operation opens a fresh connection.
	if _, err := db.Exec("CREATE TABLE example (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	if _, err := db.Exec("INSERT INTO example VALUES (1)"); err != nil {
		t.Fatal("pool reopened against a different directory:", err)
	}
	var path string
	var seq int
	var name string
	if err := db.QueryRow("PRAGMA database_list").Scan(&seq, &name, &path); err != nil || path != filepath.Join(base, "a b.db") {
		t.Fatalf("database path=%q error=%v", path, err)
	}
}
