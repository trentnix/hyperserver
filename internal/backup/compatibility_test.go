package backup

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dbsqlite "github.com/trentnix/hyperserver/pkg/database/sqlite"
	sessionsqlite "github.com/trentnix/hyperserver/pkg/services/session/sqlite"
)

func TestIncompatibleBackupsLeaveOriginalsUntouched(t *testing.T) {
	cfg := fixture(t)
	archive := filepath.Join(t.TempDir(), "valid.zip")
	if err := Create(t.Context(), cfg, archive); err != nil {
		t.Fatal(err)
	}
	originals := map[string][]byte{}
	for _, name := range []string{"accounts.db", "blog.db", "uploads/test.png", "config.yaml", "modules/blog/config.yaml"} {
		filename := filepath.Join(cfg.App.WorkingDirectory, name)
		data, err := os.ReadFile(filename)
		if err != nil {
			t.Fatal(err)
		}
		originals[filename] = data
	}
	for _, role := range []struct{ name, file, table string }{
		{"blog", "databases/blog.db", "blog_schema_version"},
		{"accounts", "databases/accounts.db", "hyperserver_account_version"},
		{"sessions", "databases/accounts.db", "hyperserver_session_version_sessions"},
	} {
		for _, change := range []string{
			"UPDATE %s SET version=0", "UPDATE %s SET version=2", "UPDATE %s SET version=3",
			"UPDATE %s SET dirty=true", "DROP TABLE %s", "DELETE FROM %s",
			"INSERT INTO %s VALUES (2, false)", "UPDATE %s SET version=NULL",
			"UPDATE %s SET dirty=NULL", "ALTER TABLE %s RENAME COLUMN version TO missing",
		} {
			t.Run(role.name+"/"+change, func(t *testing.T) {
				extracted, _ := unpack(t, archive)
				filename := filepath.Join(extracted, filepath.FromSlash(role.file))
				db, err := dbsqlite.Open(filename)
				if err != nil {
					t.Fatal(err)
				}
				_, err = db.ExecContext(t.Context(), fmt.Sprintf(change, role.table))
				closeErr := db.Close()
				if err != nil || closeErr != nil {
					t.Fatal(err, closeErr)
				}
				data, err := os.ReadFile(filename)
				if err != nil {
					t.Fatal(err)
				}
				bad := filepath.Join(t.TempDir(), "incompatible.zip")
				rewriteArchive(t, archive, bad, func(name string, original []byte) (string, []byte, os.FileMode) {
					if name == role.file {
						return name, data, 0600
					}
					if name == "manifest.json" {
						var m manifest
						if err := json.Unmarshal(original, &m); err != nil {
							t.Fatal(err)
						}
						for i := range m.Files {
							if m.Files[i].Path == role.file {
								digest := sha256.Sum256(data)
								m.Files[i].Size, m.Files[i].SHA256 = int64(len(data)), hex.EncodeToString(digest[:])
							}
						}
						original, err = json.Marshal(m)
						if err != nil {
							t.Fatal(err)
						}
					}
					return name, original, 0600
				})
				before, err := os.ReadFile(bad)
				if err != nil {
					t.Fatal(err)
				}
				if err := Verify(t.Context(), bad); err == nil || !strings.Contains(err.Error(), "incompatible "+role.name+" backup") {
					t.Fatalf("compatibility error = %v", err)
				}
				after, err := os.ReadFile(bad)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatal("verification changed the archive", err)
				}
				for filename, before := range originals {
					after, err := os.ReadFile(filename)
					if err != nil || !bytes.Equal(before, after) {
						t.Fatal("verification changed source data", filename, err)
					}
				}
			})
		}
	}
}

func TestBackupCompatibilityCustomSessionTable(t *testing.T) {
	cfg := fixture(t)
	const table = `custom"sessions`
	store, err := sessionsqlite.NewSQLiteStore(t.Context(), sessionsqlite.Options{
		Connection: filepath.Join(cfg.App.WorkingDirectory, "accounts.db"), Table: table,
		TokenLifetime: time.Hour, CookieLifetime: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	for key := range cfg.HTTP.Session.Stores {
		cfg.HTTP.Session.Stores[key]["table"] = table
	}
	archive := filepath.Join(t.TempDir(), "custom.zip")
	if err := Create(t.Context(), cfg, archive); err != nil {
		t.Fatal(err)
	}
	if err := Verify(t.Context(), archive); err != nil {
		t.Fatal(err)
	}
}

func TestIncompatibleSourceDoesNotPublishBackup(t *testing.T) {
	cfg := fixture(t)
	filename := filepath.Join(cfg.App.WorkingDirectory, "blog.db")
	db, err := dbsqlite.Open(filename)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(t.Context(), `UPDATE blog_schema_version SET version=3`)
	closeErr := db.Close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	before, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "incompatible.zip")
	if err := Create(t.Context(), cfg, destination); err == nil || !strings.Contains(err.Error(), "incompatible blog backup") {
		t.Fatalf("backup error = %v", err)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatal("incompatible backup was published", err)
	}
	after, err := os.ReadFile(filename)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed backup changed source data", err)
	}
}
