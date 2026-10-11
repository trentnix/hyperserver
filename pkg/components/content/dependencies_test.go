package content

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestPresentationPackagesDoNotImportStorage(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	// Inspect production imports, not test dependencies. Forms and templates must
	// remain usable without loading account, session, mail, or SQL implementations.
	cmd := exec.CommandContext(ctx, "go", "list", "-deps",
		"github.com/trentnix/hyperserver/pkg/components/content",
		"github.com/trentnix/hyperserver/pkg/components/form",
		"github.com/trentnix/hyperserver/pkg/services/content",
	)
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("inspect presentation dependencies: %v", err)
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
			if dependency == forbidden || strings.HasPrefix(dependency, forbidden+"/") {
				t.Errorf("presentation imports optional dependency %s", dependency)
			}
		}
	}
}
