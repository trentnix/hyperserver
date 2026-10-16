package auth

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestAuthenticationPackagesDoNotImportStorageProviders(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	// Include authentication subpackages and transitive production dependencies,
	// but not test imports used to exercise real storage providers.
	output, err := exec.CommandContext(ctx, "go", "list", "-deps", "./...").CombinedOutput()
	if err != nil {
		t.Fatalf("inspect authentication dependencies: %v\n%s", err, output)
	}
	for _, dependency := range strings.Fields(string(output)) {
		if dependency == "database/sql" {
			t.Error("authentication imports database/sql")
		}
		for _, provider := range []string{
			"github.com/trentnix/hyperserver/pkg/database",
			"github.com/trentnix/hyperserver/pkg/services/user/sqlite",
			"github.com/trentnix/hyperserver/pkg/services/session/sqlite",
			"github.com/trentnix/hyperserver/pkg/services/session/cookie",
			"github.com/jmoiron/sqlx",
			"github.com/mattn/go-sqlite3",
		} {
			if dependency == provider || strings.HasPrefix(dependency, provider+"/") {
				t.Errorf("authentication imports storage dependency %s", dependency)
			}
		}
	}
}
