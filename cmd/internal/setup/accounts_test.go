package setup

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestAccountSetupAndCleanupDoNotImportServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	output, err := exec.CommandContext(ctx, "go", "list", "-deps",
		"github.com/trentnix/hyperserver/cmd/internal/setup",
		"github.com/trentnix/hyperserver/cmd/cleanup").CombinedOutput()
	if err != nil {
		t.Fatalf("inspect dependencies: %v\n%s", err, output)
	}
	for _, dependency := range strings.Fields(string(output)) {
		if dependency == "github.com/trentnix/hyperserver/pkg/server" {
			t.Fatal("account setup or cleanup imports the web server")
		}
	}
}
