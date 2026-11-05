package deployment

import (
	"archive/tar"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/internal/maintenance"
)

func TestMaintenanceRejectsUnsafeInputsBeforeDocker(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("local deployment scripts require Linux")
	}
	script, err := os.ReadFile("../../../deploy/local/storage.sh")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, subdir := range []string{"state/data", "state/backups", "bin"} {
		if err := os.MkdirAll(filepath.Join(dir, subdir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for name, contents := range map[string][]byte{
		"storage.sh":                   script,
		".env":                         []byte(""),
		"state/backups/existing.db":    []byte("fixture"),
		"state/backups/incomplete.db":  []byte("fixture"),
		"state/backups/incomplete.env": []byte("fixture"),
		"bin/docker":                   []byte("#!/bin/sh\necho unexpected-docker-call >&2\nexit 99\n"),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), contents, 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", filepath.Join(dir, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, tc := range []struct{ action, name, message string }{
		{"backup", "../outside", "Backup names"},
		{"delete", "safe", "Use backup or restore"},
		{"backup", "existing", "already exists"},
		{"restore", "absent", "missing or incomplete"},
		{"restore", "incomplete", "missing or incomplete"},
	} {
		t.Run(tc.action+"/"+tc.name, func(t *testing.T) {
			cmd := exec.CommandContext(t.Context(), "bash", filepath.Join(dir, "storage.sh"), tc.action, tc.name)
			out, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(out), tc.message) || strings.Contains(string(out), "unexpected-docker-call") {
				t.Fatalf("%v: %s", err, out)
			}
		})
	}
}

// Exercise filesystem orchestration without Docker or the developer's rehearsal.
// SQLite snapshot correctness is covered separately by backup and migration tests.
func TestMaintenanceRestoresBothDatabasesAndUploads(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("local deployment scripts require Linux")
	}
	script, err := os.ReadFile("../../../deploy/local/storage.sh")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	write := func(name, value string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), 0700); err != nil {
			t.Fatal(err)
		}
	}
	write("storage.sh", string(script))
	write(".env", "")
	write("state/app.env", "original keys")
	write("state/data/hyperserver.db", "original accounts")
	write("state/data/blog.db", "original posts")
	write("state/data/uploads/image", "original image")
	if err := os.MkdirAll(filepath.Join(dir, "state/backups"), 0700); err != nil {
		t.Fatal(err)
	}
	write("bin/docker", `#!/usr/bin/env bash
set -euo pipefail
if [[ "$*" == *" stop app" ]]; then exit 0; fi
if flock -n "$FIXTURE_ROOT/state/data/.blog-maintenance.lock" true; then
  echo 'Storage operation ran without the blog lock.' >&2
  exit 98
fi
while [[ "$1" != sqlite3 ]]; do shift; done
database="$FIXTURE_ROOT/state/${2#/}"
case "$3" in
  .backup*|.restore*)
    archive=${3#*\'}
    archive=${archive%\'}
    archive="$FIXTURE_ROOT/state/${archive#/}"
    if [[ "$3" == .backup* ]]; then cp "$database" "$archive"; else cp "$archive" "$database"; fi ;;
  *) printf 'ok\n' ;;
esac
`)
	t.Setenv("FIXTURE_ROOT", dir)
	t.Setenv("PATH", filepath.Join(dir, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
	run := func(action string) {
		t.Helper()
		// A server or another offline command must prevent either operation.
		lock, err := maintenance.Lock(filepath.Join(dir, "state/data"))
		if err != nil {
			t.Fatal(err)
		}
		blocked := exec.CommandContext(t.Context(), "bash", filepath.Join(dir, "storage.sh"), action, "snapshot")
		output, runErr := blocked.CombinedOutput()
		if err := lock.Close(); err != nil {
			t.Fatal(err)
		}
		if runErr == nil || !strings.Contains(string(output), "Blog or maintenance is already running") {
			t.Fatalf("%s ignored the blog lock: %v %s", action, runErr, output)
		}
		cmd := exec.CommandContext(t.Context(), "bash", filepath.Join(dir, "storage.sh"), action, "snapshot")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v %s", action, err, output)
		}
		lock, err = maintenance.Lock(filepath.Join(dir, "state/data"))
		if err != nil {
			t.Fatal("completed operation retained the blog lock", err)
		}
		lock.Close()
	}
	run("backup")
	write("state/app.env", "changed keys")
	write("state/data/hyperserver.db", "changed accounts")
	write("state/data/blog.db", "changed posts")
	write("state/data/uploads/image", "changed image")
	write("state/data/uploads/extra", "later image")
	run("restore")
	assertRestored := func() {
		t.Helper()
		for name, want := range map[string]string{
			"state/app.env": "original keys", "state/data/hyperserver.db": "original accounts",
			"state/data/blog.db": "original posts", "state/data/uploads/image": "original image",
		} {
			got, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil || string(got) != want {
				t.Fatalf("%s: %q %v", name, got, err)
			}
		}
	}
	assertRestored()
	if _, err := os.Stat(filepath.Join(dir, "state/data/uploads/extra")); !os.IsNotExist(err) {
		t.Fatal("restore retained a later upload", err)
	}
	previous, err := filepath.Glob(filepath.Join(dir, "state/data/restore-*/previous-uploads/extra"))
	if err != nil || len(previous) != 1 {
		t.Fatal("replaced uploads are not recoverable", previous, err)
	}

	// Recovery must also work when the original uploads directory is gone.
	if err := os.Rename(filepath.Join(dir, "state/data/uploads"), filepath.Join(dir, "lost-uploads")); err != nil {
		t.Fatal(err)
	}
	write("state/data/blog.db", "changed posts")
	run("restore")
	assertRestored()

	// Both malformed archives and valid archives without uploads must fail
	// before changing either database, existing uploads, or keys.
	var empty bytes.Buffer
	if err := tar.NewWriter(&empty).Close(); err != nil {
		t.Fatal(err)
	}
	for _, archive := range []string{"invalid tar", empty.String()} {
		write("state/backups/snapshot.uploads.tar", archive)
		unchanged := map[string]string{
			"state/app.env": "current keys", "state/data/hyperserver.db": "current accounts",
			"state/data/blog.db": "current posts", "state/data/uploads/image": "current image",
		}
		for name, value := range unchanged {
			write(name, value)
		}
		cmd := exec.CommandContext(t.Context(), "bash", filepath.Join(dir, "storage.sh"), "restore", "snapshot")
		if output, err := cmd.CombinedOutput(); err == nil {
			t.Fatalf("invalid uploads archive accepted: %s", output)
		}
		lock, err := maintenance.Lock(filepath.Join(dir, "state/data"))
		if err != nil {
			t.Fatal("failed restore retained the blog lock", err)
		}
		lock.Close()
		for name, want := range unchanged {
			got, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil || string(got) != want {
				t.Fatalf("failed restore changed %s: %q %v", name, got, err)
			}
		}
	}
}
