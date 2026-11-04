package providercontracts_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A separate module catches internal-package restrictions that a package named
// *_test inside the HyperServer module would not catch.
func TestExternalImports(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, name := range []string{"go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if name == "go.mod" {
			// Reuse the repository's toolchain and dependency versions without
			// downloading modules or editing the repository's module files.
			mod := strings.Replace(string(data), "module github.com/trentnix/hyperserver", "module example.com/provider-contracts", 1)
			data = []byte(mod + fmt.Sprintf("\nrequire github.com/trentnix/hyperserver v0.0.0\nreplace github.com/trentnix/hyperserver => %q\n", root))
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	source, err := os.ReadFile("testdata/imports_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "imports_test.go"), source, 0600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "test", "-mod=readonly", "-run=^$", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off", "GOSUMDB=off")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile external provider tests: %v\n%s", err, output)
	}
}
