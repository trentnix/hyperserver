package externalmodule

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// A distinct module path makes Go enforce the same internal-package boundary
// that an application outside this repository encounters.
func TestExternalModule(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err := os.CopyFS(directory, os.DirFS(filepath.Join(root, "examples/external"))); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, "go", args...)
		cmd.Dir = directory
		cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=readonly")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %v: %v\n%s", args, err, out)
		}
	}
	// Only the temporary copy points at the checkout. The runnable example keeps
	// its public version pin and does not require a sibling HyperServer directory.
	run("mod", "edit", "-replace=github.com/trentnix/hyperserver="+root)
	run("test", "-count=1", "./...")
	// The copied fixture has no repository history to stamp into the executable.
	run("build", "-buildvcs=false", "-o", filepath.Join(t.TempDir(), "greeting"), ".")
}
