package messaging

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestSharedPackagesDoNotImportSMTP(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	output, err := exec.CommandContext(ctx, "go", "list", "-deps",
		"github.com/trentnix/hyperserver/pkg/services/messaging",
		"github.com/trentnix/hyperserver/pkg/server",
	).CombinedOutput()
	if err != nil {
		t.Fatalf("inspect shared dependencies: %v\n%s", err, output)
	}
	for _, dependency := range strings.Fields(string(output)) {
		switch dependency {
		case "net/smtp", "github.com/trentnix/hyperserver/pkg/services/messaging/smtp":
			t.Fatalf("shared packages import SMTP delivery: %s", dependency)
		}
	}
}
