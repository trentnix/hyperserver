package setup

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestAccountMaintenanceDoesNotOpenRuntimeStorage(t *testing.T) {
	cfg := &config.Config{Auth: config.AuthConfig{Enabled: true, AccountStorage: config.AccountStorageConfig{Provider: "custom"}}}
	closes := 0
	providers := map[string]user.StorageProvider{"custom": {
		Open: func(context.Context, map[string]string) (user.AccountRepository, func() error, error) {
			t.Fatal("maintenance opened runtime storage")
			return nil, nil, nil
		},
		OpenMaintenance: func(context.Context, map[string]string) (user.TokenMaintenance, func() error, error) {
			return tokenMaintenanceFunc(func(context.Context, int) (int64, error) { return 1, nil }), func() error { closes++; return nil }, nil
		},
	}}
	if count, err := cleanupAccountTokens(t.Context(), cfg, 1, providers); count != 1 || err != nil || closes != 1 {
		t.Fatalf("maintenance: count=%d closes=%d err=%v", count, closes, err)
	}
}

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
