package database

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestSharedPackagesDoNotImportSQLite(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	output, err := exec.CommandContext(ctx, "go", "list", "-deps",
		"github.com/trentnix/hyperserver/pkg/database",
		"github.com/trentnix/hyperserver/pkg/server",
		"github.com/trentnix/hyperserver/examples/stateless",
	).CombinedOutput()
	if err != nil {
		t.Fatalf("inspect shared dependencies: %v\n%s", err, output)
	}
	for _, dependency := range strings.Fields(string(output)) {
		if dependency == "github.com/mattn/go-sqlite3" {
			t.Fatal("shared packages import the SQLite driver")
		}
	}
}

func TestCoreDoesNotImportSQLPool(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	output, err := exec.CommandContext(ctx, "go", "list", "-deps",
		"github.com/trentnix/hyperserver/pkg/server",
		"github.com/trentnix/hyperserver/examples/stateless",
	).CombinedOutput()
	if err != nil {
		t.Fatalf("inspect core dependencies: %v\n%s", err, output)
	}
	for _, dependency := range strings.Fields(string(output)) {
		switch dependency {
		case "github.com/jmoiron/sqlx", "github.com/trentnix/hyperserver/pkg/database", "database/sql":
			t.Fatalf("core imports SQL pool dependency %q", dependency)
		}
	}
}
