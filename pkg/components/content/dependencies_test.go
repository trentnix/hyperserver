package content

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestCorePackagesDoNotImportStorage(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	// Inspect production imports, not test dependencies. Rendering, forms, and
	// general HTTP middleware must not load optional service implementations.
	cmd := exec.CommandContext(ctx, "go", "list", "-deps",
		"github.com/trentnix/hyperserver/pkg/components/content",
		"github.com/trentnix/hyperserver/pkg/components/form",
		"github.com/trentnix/hyperserver/pkg/services/content",
		"github.com/trentnix/hyperserver/pkg/services/middleware",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("inspect core dependencies: %v\n%s", err, output)
	}
	for _, dependency := range strings.Fields(string(output)) {
		for _, forbidden := range []string{
			"github.com/trentnix/hyperserver/auth",
			"github.com/trentnix/hyperserver/pkg/database",
			"github.com/trentnix/hyperserver/pkg/services/flash",
			"github.com/trentnix/hyperserver/pkg/services/session",
			"github.com/trentnix/hyperserver/pkg/services/user",
			"github.com/trentnix/hyperserver/pkg/services/messaging",
			"github.com/jmoiron/sqlx",
			"github.com/mattn/go-sqlite3",
			"github.com/golang-jwt/jwt",
			"database/sql",
			"net/smtp",
		} {
			// Match whole provider trees, but only the named standard-library packages.
			// UUID's sql/driver support alone does not load database/sql or a provider.
			providerPackage := strings.HasPrefix(forbidden, "github.com/") && strings.HasPrefix(dependency, forbidden+"/")
			if dependency == forbidden || providerPackage {
				t.Errorf("core imports optional dependency %s", dependency)
			}
		}
	}
}
