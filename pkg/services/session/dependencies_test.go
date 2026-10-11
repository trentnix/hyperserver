package session

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestSessionPackageDoesNotImportProviders(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	output, err := exec.CommandContext(ctx, "go", "list", "-deps", "github.com/trentnix/hyperserver/pkg/services/session").CombinedOutput()
	if err != nil {
		t.Fatalf("inspect session dependencies: %v\n%s", err, output)
	}
	for _, dependency := range strings.Fields(string(output)) {
		for _, provider := range []string{
			"github.com/trentnix/hyperserver/pkg/services/session/stores",
			"github.com/trentnix/hyperserver/pkg/database",
			"github.com/jmoiron/sqlx",
			"github.com/mattn/go-sqlite3",
			"github.com/golang-jwt/jwt/v5",
		} {
			if dependency == provider || strings.HasPrefix(dependency, provider+"/") {
				t.Errorf("session contract imports provider %s", dependency)
			}
		}
	}
}
