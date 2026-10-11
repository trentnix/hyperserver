package database

import (
	"net/url"
	"path/filepath"
	"testing"
)

func TestResolveConnection(t *testing.T) {
	base := t.TempDir()
	uri := (&url.URL{Scheme: "file", Path: filepath.ToSlash(filepath.Join(base, "a b.db"))}).String()
	for _, test := range []struct{ driver, input, want string }{
		{"sqlite3", "data.db", filepath.Join(base, "data.db")},
		{"sqlite3", "data.db?_busy_timeout=50", filepath.Join(base, "data.db") + "?_busy_timeout=50"},
		{"sqlite3", "file:a%20b.db?mode=rwc&cache=shared", uri + "?mode=rwc&cache=shared"},
		{"sqlite3", uri + "?mode=rw", uri + "?mode=rw"},
		{"sqlite3", filepath.Join(base, "absolute.db"), filepath.Join(base, "absolute.db")},
		{"sqlite3", "", ""},
		{"sqlite3", "?_busy_timeout=50", "?_busy_timeout=50"},
		{"sqlite3", ":memory:", ":memory:"},
		{"sqlite3", ":memory:?_busy_timeout=50", ":memory:?_busy_timeout=50"},
		{"sqlite3", "file::memory:?cache=shared", "file::memory:?cache=shared"},
		{"sqlite3", "file:%3Amemory:?cache=shared", "file:%3Amemory:?cache=shared"},
		{"sqlite3", "file:shared?mode=memory&cache=shared", "file:shared?mode=memory&cache=shared"},
		{"postgres", "host=db dbname=app", "host=db dbname=app"},
	} {
		t.Run(test.input, func(t *testing.T) {
			got, err := ResolveConnection(test.driver, test.input, base)
			if err != nil || got != test.want {
				t.Fatalf("connection=%q want=%q error=%v", got, test.want, err)
			}
		})
	}
	if _, err := ResolveConnection("sqlite3", "file:bad%xy.db", base); err == nil {
		t.Fatal("invalid URI accepted")
	}
}
