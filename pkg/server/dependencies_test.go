package server

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestCoreDoesNotImportAccountProviders(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	output, err := exec.CommandContext(ctx, "go", "list", "-deps", "github.com/trentnix/hyperserver/pkg/server").CombinedOutput()
	if err != nil {
		t.Fatalf("inspect core dependencies: %v\n%s", err, output)
	}
	for _, dependency := range strings.Fields(string(output)) {
		for _, forbidden := range []string{
			"github.com/trentnix/hyperserver/pkg/services/user/sqlite",
			"github.com/trentnix/hyperserver/internal/reference",
		} {
			if dependency == forbidden || strings.HasPrefix(dependency, forbidden+"/") {
				t.Errorf("core imports account setup or provider %s", dependency)
			}
		}
	}
}
