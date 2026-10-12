package models

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestContactModelDoesNotImportStorage(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	output, err := exec.CommandContext(ctx, "go", "list", "-deps", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("inspect contact model dependencies: %v\n%s", err, output)
	}
	for _, dependency := range strings.Fields(string(output)) {
		for _, forbidden := range []string{
			"database/sql",
			"github.com/jmoiron/sqlx",
			"github.com/mattn/go-sqlite3",
			"github.com/trentnix/hyperserver/modules/site/storage",
			"github.com/trentnix/hyperserver/modules/site/database",
			"github.com/trentnix/hyperserver/pkg/database",
		} {
			if dependency == forbidden || strings.HasPrefix(dependency, forbidden+"/") {
				t.Errorf("contact model imports storage dependency %s", dependency)
			}
		}
	}
}
