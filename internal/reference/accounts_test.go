package reference

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

type suppliedAccountRepository struct{ user.AccountRepository }

func TestAccountOpenTransfersOwnership(t *testing.T) {
	s := accountProviderApplication("custom")
	repository := &suppliedAccountRepository{}
	opens, closes, maintenanceCloses := 0, 0, 0
	if err := s.RegisterProvider("custom", AccountProvider{
		Open: func(context.Context, map[string]string) (user.AccountRepository, func() error, error) {
			opens++
			return repository, func() error { closes++; return nil }, nil
		},
		OpenMaintenance: func(context.Context, map[string]string) (user.TokenMaintenance, func() error, error) {
			return tokenMaintenanceFunc(func(context.Context, int) (int64, error) {
				return 1, nil
			}), func() error { maintenanceCloses++; return nil }, nil
		},
	}); err != nil {
		t.Fatal(err)
	}

	got, closeStore, err := s.Open(t.Context(), nil)
	if err != nil || got != repository || closeStore == nil || opens != 1 || closes != 0 {
		t.Fatalf("open: repository=%v, cleanup=%t, opens=%d, closes=%d, error=%v", got, closeStore != nil, opens, closes, err)
	}
	if count, err := s.CleanupTokens(t.Context(), 1); count != 1 || err != nil {
		t.Fatalf("maintenance: count=%d, error=%v", count, err)
	}
	if opens != 1 || closes != 0 || maintenanceCloses != 1 {
		t.Fatalf("maintenance changed runtime ownership: opens=%d, closes=%d, maintenance closes=%d", opens, closes, maintenanceCloses)
	}
	if err := closeStore(); err != nil || closes != 1 {
		t.Fatalf("caller cleanup: closes=%d, error=%v", closes, err)
	}
}

func TestAccountSetupAndCleanupDoNotImportServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	output, err := exec.CommandContext(ctx, "go", "list", "-deps",
		"github.com/trentnix/hyperserver/internal/reference",
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
