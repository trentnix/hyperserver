package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/internal/maintenance"
	dbsqlite "github.com/trentnix/hyperserver/pkg/database/sqlite"
	"gopkg.in/yaml.v3"
)

func fixture(t *testing.T) config.Config {
	t.Helper()
	template, err := os.ReadFile("../../config-template.yaml")
	if err != nil {
		t.Fatal(err)
	}
	module, err := os.ReadFile("../../modules/blog/config-template.yaml")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	write := func(name string, data []byte) {
		t.Helper()
		target := filepath.Join(directory, name)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	template = []byte(strings.ReplaceAll(strings.ReplaceAll(string(template), "REPLACE_WITH_RANDOM_SESSION_KEY", strings.Repeat("s", 64)), "REPLACE_WITH_DIFFERENT_RANDOM_AUTH_KEY", strings.Repeat("a", 64)))
	write("config.yaml", template)
	write("modules/blog/config.yaml", module)
	write("uploads/test.png", []byte("original image bundle"))
	if err := os.Mkdir(filepath.Join(directory, "uploads", "empty"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)
	cfg, err := config.GetConfig()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"accounts.db", "blog.db"} {
		db, err := dbsqlite.OpenConfigured(config.DatabaseConfig{Driver: "sqlite3", Connection: name + "?_journal=WAL"}, directory)
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.Exec("CREATE TABLE recovery (id INTEGER PRIMARY KEY, value TEXT NOT NULL); INSERT INTO recovery VALUES (1, 'saved')")
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return cfg
}

// unpack uses only the standard ZIP reader, like an ordinary unzip utility.
func unpack(t *testing.T, archive string) (string, manifest) {
	t.Helper()
	if err := Verify(t.Context(), archive); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	reader, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	for _, file := range reader.File {
		target := filepath.Join(directory, filepath.FromSlash(file.Name))
		if file.Mode().IsDir() {
			if err := os.MkdirAll(target, 0700); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		in, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			in.Close()
			t.Fatal(err)
		}
		_, copyErr := io.Copy(out, in)
		in.Close()
		closeErr := out.Close()
		if err := errors.Join(copyErr, closeErr); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return directory, m
}

// recoverFixture follows the documented manual placement procedure for this fixture.
// No production restore code or configuration rewriting is involved.
func recoverFixture(t *testing.T, archive string, cfg config.Config) string {
	t.Helper()
	extracted, m := unpack(t, archive)
	destination := t.TempDir()
	for _, resource := range m.Resources {
		relative, err := filepath.Rel(cfg.App.WorkingDirectory, resource.Source)
		if err != nil || !filepath.IsLocal(relative) {
			t.Fatal("fixture resource is not local", resource.Source, err)
		}
		source := filepath.Join(extracted, filepath.FromSlash(resource.Path))
		target := filepath.Join(destination, relative)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(source, target); err != nil {
			t.Fatal(err)
		}
	}
	return destination
}

func TestRoundTrip(t *testing.T) {
	cfg := fixture(t)
	source := cfg.App.WorkingDirectory
	originalConfig, err := os.ReadFile(filepath.Join(source, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Mail.Password = "environment-secret-not-archived"
	archive := filepath.Join(t.TempDir(), "backup.zip")
	if err := Create(t.Context(), cfg, archive); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(archive)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal(info, err)
	}
	extracted, m := unpack(t, archive)
	configCopy, err := os.ReadFile(filepath.Join(extracted, "config.yaml"))
	if err != nil || !bytes.Equal(configCopy, originalConfig) {
		t.Fatal("configuration was rewritten", err)
	}
	if bytes.Contains(configCopy, []byte(cfg.Mail.Password)) {
		t.Fatal("environment secret unexpectedly archived")
	}
	if len(m.Resources[1].Roles) != 2 {
		t.Fatal("shared account/session database not recorded", m.Resources)
	}
	restored := recoverFixture(t, archive, cfg)
	t.Chdir(restored)
	got, err := config.GetConfig()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"blog.db", "accounts.db"} {
		db, err := openDatabase(filepath.Join(restored, name))
		if err != nil {
			t.Fatal(err)
		}
		var value string
		err = db.QueryRow("SELECT value FROM recovery").Scan(&value)
		db.Close()
		if err != nil || value != "saved" {
			t.Fatal(value, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(restored, "uploads", "test.png"))
	if err != nil || string(data) != "original image bundle" {
		t.Fatal(string(data), err)
	}
	if info, err := os.Stat(filepath.Join(restored, "uploads", "empty")); err != nil || !info.IsDir() {
		t.Fatal("empty directory lost")
	}
	if err := Create(t.Context(), got, filepath.Join(t.TempDir(), "second.zip")); err != nil {
		t.Fatal(err)
	}
}

func TestBackupFailures(t *testing.T) {
	cfg := fixture(t)
	directory := t.TempDir()
	for _, scenario := range []string{"canceled", "locked", "missing", "invalid-db", "symlink", "inside-uploads", "unsupported", "nil-options"} {
		t.Run(scenario, func(t *testing.T) {
			current := cfg.Clone()
			ctx := t.Context()
			output := filepath.Join(directory, scenario+".zip")
			switch scenario {
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "locked":
				lock, err := maintenance.Lock(cfg.App.WorkingDirectory)
				if err != nil {
					t.Fatal(err)
				}
				defer lock.Close()
			case "missing":
				current.Auth.AccountStorage.Options["connection"] = "missing.db"
			case "invalid-db":
				// Discovery succeeds, but snapshotting fails after output reservation.
				invalid := filepath.Join(t.TempDir(), "invalid.db")
				if err := os.WriteFile(invalid, []byte("not sqlite"), 0600); err != nil {
					t.Fatal(err)
				}
				current.Auth.AccountStorage.Options["connection"] = invalid
			case "symlink":
				link := filepath.Join(cfg.App.WorkingDirectory, "uploads", "link")
				if err := os.Symlink("test.png", link); err != nil {
					t.Fatal(err)
				}
				defer os.Remove(link)
			case "inside-uploads":
				output = filepath.Join(cfg.App.WorkingDirectory, "uploads", "backup.zip")
			case "unsupported":
				current.Auth.AccountStorage.Provider = "postgres"
			case "nil-options":
				current.Auth.AccountStorage.Options = nil
			}
			if err := Create(ctx, current, output); err == nil {
				t.Fatal("invalid backup succeeded")
			}
			if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("failed backup left an output", err)
			}
			if stages, _ := filepath.Glob(filepath.Join(directory, ".blog-backup-*")); len(stages) != 0 {
				t.Fatal("failed backup left staging data", stages)
			}
			if _, err := os.Stat(filepath.Join(cfg.App.WorkingDirectory, "missing.db")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("created missing source database")
			}
		})
	}
}

func TestWALSnapshot(t *testing.T) {
	cfg := fixture(t)
	pool, err := dbsqlite.OpenConfigured(config.DatabaseConfig{Driver: "sqlite3", Connection: "blog.db?_journal=WAL"}, cfg.App.WorkingDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec("PRAGMA wal_autocheckpoint=0; UPDATE recovery SET value='in WAL'"); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "wal.zip")
	if err := Create(t.Context(), cfg, archive); err != nil {
		t.Fatal(err)
	}
	restored := recoverFixture(t, archive, cfg)
	db, err := openDatabase(filepath.Join(restored, "blog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var value string
	if err := db.QueryRow("SELECT value FROM recovery").Scan(&value); err != nil || value != "in WAL" {
		t.Fatal(value, err)
	}
}

// rewriteArchive keeps tests independent of compressed offsets and CRC details.
func rewriteArchive(t *testing.T, source, target string, change func(string, []byte) (string, []byte, os.FileMode)) {
	t.Helper()
	input, err := zip.OpenReader(source)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	out, err := os.Create(target)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(out)
	for _, file := range input.File {
		in, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(in)
		in.Close()
		if err != nil {
			t.Fatal(err)
		}
		name, data, mode := change(file.Name, data)
		if name == "" {
			continue
		}
		header := &zip.FileHeader{Name: name, Modified: time.Now()}
		if strings.HasSuffix(name, "/") && mode.IsRegular() {
			mode = os.ModeDir | 0700
		}
		header.SetMode(mode)
		entry, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestUnsafeArchives(t *testing.T) {
	cfg := fixture(t)
	source := filepath.Join(t.TempDir(), "valid.zip")
	if err := Create(t.Context(), cfg, source); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"traversal", "absolute", "symlink", "duplicate", "missing", "checksum", "format", "oversized", "invalid-db", "missing-manifest", "invalid-manifest", "directory-traversal", "duplicate-directory", "duplicate-inventory", "invalid-checksum", "missing-role", "missing-resource", "duplicate-role"} {
		t.Run(scenario, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), "bad.zip")
			changed := make(map[string][]byte)
			rewriteArchive(t, source, filename, func(name string, data []byte) (string, []byte, os.FileMode) {
				if name == "uploads/test.png" {
					switch scenario {
					case "traversal":
						name = "../escaped"
					case "absolute":
						name = "/escaped"
					case "symlink":
						return name, data, os.ModeSymlink | 0600
					case "duplicate":
						name = "config.yaml"
					case "missing":
						name = ""
					case "checksum":
						data = []byte("corrupt")
					}
				}
				if name == "manifest.json" {
					if scenario == "missing-manifest" {
						return "", nil, 0600
					}
					if scenario == "invalid-manifest" {
						return name, []byte("not JSON"), 0600
					}
					var m manifest
					if err := json.Unmarshal(data, &m); err != nil {
						t.Fatal(err)
					}
					switch scenario {
					case "format":
						m.Format++
					case "oversized":
						m.Files[0].Size = maxBytes + 1
					case "directory-traversal":
						m.Directories = append(m.Directories, "../escaped")
					case "duplicate-directory":
						m.Directories = append(m.Directories, m.Directories[0])
					case "duplicate-inventory":
						m.Files[1] = m.Files[0]
					case "invalid-checksum":
						m.Files[0].SHA256 = "not a checksum"
					case "missing-role":
						m.Resources[0].Roles = nil
					case "missing-resource":
						m.Resources[0].Path = "missing.db"
					case "duplicate-role":
						m.Resources[1].Roles = append(m.Resources[1].Roles, "blog")
					}
					// Exercise configuration/database validation, not just checksums.
					for i := range m.Files {
						if replacement, ok := changed[m.Files[i].Path]; ok {
							digest := sha256.Sum256(replacement)
							m.Files[i].Size = int64(len(replacement))
							m.Files[i].SHA256 = hex.EncodeToString(digest[:])
						}
					}
					data, _ = json.Marshal(m)
				}
				if scenario == "invalid-db" && name == "databases/blog.db" {
					data = []byte("not sqlite")
					changed[name] = data
				}
				return name, data, 0600
			})
			if err := Verify(t.Context(), filename); err == nil {
				t.Fatal("unsafe archive verified")
			}
		})
	}
}

func TestAssetsAndTLS(t *testing.T) {
	cfg := fixture(t)
	assets := filepath.Join(cfg.App.WorkingDirectory, "assets")
	if err := os.Mkdir(assets, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assets, "icon.svg"), []byte("<svg/>"), 0600); err != nil {
		t.Fatal(err)
	}
	settings, err := loadSettings(cfg.App.WorkingDirectory)
	if err != nil {
		t.Fatal(err)
	}
	settings.AssetsDirectory = "assets"
	encoded, err := yaml.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.App.WorkingDirectory, "modules", "blog", "config.yaml"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	cfg.HTTP.TLS.Enabled = true
	cfg.HTTP.TLS.Certificate = filepath.Join(t.TempDir(), "certificate.pem")
	cfg.HTTP.TLS.Key = filepath.Join(t.TempDir(), "private.pem")
	for _, name := range []string{cfg.HTTP.TLS.Certificate, cfg.HTTP.TLS.Key} {
		if err := os.WriteFile(name, []byte("private TLS data"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	archive := filepath.Join(t.TempDir(), "assets.zip")
	if err := Create(t.Context(), cfg, archive); err != nil {
		t.Fatal(err)
	}
	extracted, _ := unpack(t, archive)
	for name, want := range map[string]string{"assets/icon.svg": "<svg/>", "tls/certificate.pem": "private TLS data", "tls/key.pem": "private TLS data"} {
		got, err := os.ReadFile(filepath.Join(extracted, name))
		if err != nil || string(got) != want {
			t.Fatal(name, string(got), err)
		}
	}
}

func TestSeparateSessionDatabase(t *testing.T) {
	cfg := fixture(t)
	source := filepath.Join(t.TempDir(), "sessions.db")
	db, err := dbsqlite.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("CREATE TABLE separate_session (value TEXT); INSERT INTO separate_session VALUES ('session')")
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	for key := range cfg.HTTP.Session.Stores {
		if strings.EqualFold(key, "sqlitestore") {
			cfg.HTTP.Session.Stores[key]["connection"] = source
		}
	}
	archive := filepath.Join(t.TempDir(), "backup.zip")
	if err := Create(t.Context(), cfg, archive); err != nil {
		t.Fatal(err)
	}
	extracted, m := unpack(t, archive)
	if m.Resources[2].Source != source || len(m.Resources[2].Roles) != 1 || m.Resources[2].Roles[0] != "sessions" {
		t.Fatal("separate session database missing from inventory", m.Resources)
	}
	db, err = openDatabase(filepath.Join(extracted, "databases", "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var value string
	if err := db.QueryRow("SELECT value FROM separate_session").Scan(&value); err != nil || value != "session" {
		t.Fatal(value, err)
	}
}

func TestCanceledVerification(t *testing.T) {
	cfg := fixture(t)
	archive := filepath.Join(t.TempDir(), "backup.zip")
	if err := Create(t.Context(), cfg, archive); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := Verify(ctx, archive); !errors.Is(err, context.Canceled) {
		t.Fatal("verification ignored cancellation", err)
	}
}
