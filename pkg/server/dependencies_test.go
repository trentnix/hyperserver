package server

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestCoreDoesNotImportOptionalImplementations(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	output, err := exec.CommandContext(ctx, "go", "list", "-deps", "github.com/trentnix/hyperserver/pkg/server", "github.com/trentnix/hyperserver/examples/stateless").CombinedOutput()
	if err != nil {
		t.Fatalf("inspect core dependencies: %v\n%s", err, output)
	}
	for _, dependency := range strings.Fields(string(output)) {
		for _, forbidden := range []string{
			"github.com/trentnix/hyperserver/pkg/services/user",
			"github.com/trentnix/hyperserver/internal/reference",
			"github.com/trentnix/hyperserver/pkg/services/messaging",
		} {
			if dependency == forbidden || strings.HasPrefix(dependency, forbidden+"/") {
				t.Errorf("core or stateless application imports optional setup or service %s", dependency)
			}
		}
	}
}
