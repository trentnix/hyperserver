package user

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestAccountPackageDoesNotImportBackends(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "list", "-deps", "github.com/trentnix/hyperserver/pkg/services/user")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("inspect account dependencies: %v\n%s", err, output)
	}
	for _, dependency := range strings.Fields(string(output)) {
		for _, backend := range []string{
			"github.com/trentnix/hyperserver/pkg/services/session",
			"github.com/trentnix/hyperserver/pkg/services/user/sqlite",
			"github.com/trentnix/hyperserver/pkg/database",
			"github.com/jmoiron/sqlx",
			"github.com/mattn/go-sqlite3",
		} {
			if dependency == backend || strings.HasPrefix(dependency, backend+"/") {
				t.Errorf("account package imports backend %s", dependency)
			}
		}
	}
}
