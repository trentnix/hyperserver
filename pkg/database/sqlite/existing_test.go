package sqlite

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenExisting(t *testing.T) {
	for _, format := range []string{"absolute", "relative", "URI", "relative URI", "URI rwc", "read only"} {
		t.Run(format, func(t *testing.T) {
			directory := t.TempDir()
			t.Chdir(directory)
			path := filepath.Join(directory, "session # 100%.db")
			db, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(t.Context(), "CREATE TABLE preserved (id INTEGER)"); err != nil {
				db.Close()
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}

			connection := path + "?_timeout=75"
			switch format {
			case "relative":
				connection = filepath.Base(path) + "?_timeout=75"
			case "relative URI":
				connection = "file:" + url.PathEscape(filepath.Base(path)) + "?_timeout=75"
			case "URI", "URI rwc", "read only":
				uri := &url.URL{Scheme: "file", Path: path, RawQuery: "_timeout=75"}
				connection = uri.String()
				if format == "URI rwc" {
					connection += "&mode=rwc"
				} else if format == "read only" {
					connection += "&mode=ro"
				}
			}
			db, err = OpenExisting(connection)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Close() })
			for range 2 {
				if err := db.PingContext(t.Context()); err != nil {
					t.Fatal(err)
				}
				for pragma, want := range map[string]int{"foreign_keys": 1, "busy_timeout": 75} {
					var got int
					if err := db.QueryRowContext(t.Context(), "PRAGMA "+pragma).Scan(&got); err != nil || got != want {
						t.Fatalf("%s = %d, %v, want %d", pragma, got, err, want)
					}
				}
				_, err := db.ExecContext(t.Context(), "INSERT INTO preserved VALUES (1)")
				if (err != nil) != (format == "read only") {
					t.Fatalf("write = %v", err)
				}
				// Force the next operation to open a replacement connection.
				db.SetMaxIdleConns(0)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := db.PingContext(t.Context()); err == nil {
				t.Fatal("replacement connection recreated a missing database")
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("missing file was created: %v", err)
			}
		})
	}
}

func TestOpenExistingMissingFile(t *testing.T) {
	for _, format := range []string{"path", "URI", "URI rwc"} {
		t.Run(format, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "missing.db")
			connection := path
			if format != "path" {
				connection = (&url.URL{Scheme: "file", Path: path}).String()
			}
			if format == "URI rwc" {
				connection += "?mode=rwc"
			}
			db, err := OpenExisting(connection)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if err := db.PingContext(t.Context()); err == nil {
				t.Fatal("opened a missing database")
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("missing file was created: %v", err)
			}
		})
	}
}

func TestOpenExistingRejectsInvalidConnections(t *testing.T) {
	for _, connection := range []string{
		"", ":memory:", ":memory:?cache=shared", "file::memory:",
		"file:%3Amemory%3A", "file:", "file:shared?mode=memory&cache=shared",
		"file:store?mode=rw&mode=rwc", "file:store?mode=invalid",
		"file:%zz", "store?_fk=false", "store?_timeout=invalid",
	} {
		t.Run(connection, func(t *testing.T) {
			db, err := OpenExisting(connection)
			if db != nil {
				db.Close()
			}
			if err == nil || db != nil {
				t.Fatalf("invalid connection returned %v, %v", db, err)
			}
		})
	}
}
