package deployment

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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
