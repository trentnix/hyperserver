package user

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestAccountPackageDoesNotImportSessions(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "list", "-deps", "github.com/trentnix/hyperserver/pkg/services/user")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("inspect account dependencies: %v\n%s", err, output)
	}
	for _, dependency := range strings.Fields(string(output)) {
		const sessions = "github.com/trentnix/hyperserver/pkg/services/session"
		if dependency == sessions || strings.HasPrefix(dependency, sessions+"/") {
			t.Errorf("account package imports session implementation %s", dependency)
		}
	}
}
