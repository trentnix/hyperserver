package module_site

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/server"
)

func TestSiteConfiguration(t *testing.T) {
	for _, test := range []struct {
		name, yaml string
		capacity   int
	}{
		{"missing", "", 4096},
		{"configured", "rateLimit:\n  maxClients: 7\n", 7},
		{"zero", "rateLimit:\n  maxClients: 0\n", 0},
		{"negative", "rateLimit:\n  maxClients: -1\n", 0},
		{"unknown", "rateLimit:\n  maxClient: 7\n", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := config.Config{App: config.AppConfig{WorkingDirectory: t.TempDir()}}
			if test.yaml != "" {
				directory := filepath.Join(cfg.App.WorkingDirectory, "modules", "site")
				if err := os.MkdirAll(directory, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(directory, "config.yaml"), []byte(test.yaml), 0600); err != nil {
					t.Fatal(err)
				}
			}
			app, err := server.NewApplicationServer(cfg)
			if err != nil {
				t.Fatal(err)
			}
			opened := false
			module := Module(func(context.Context) (ContactRepository, error) {
				opened = true
				return nil, nil
			}, nil).New().(*SiteModule)
			// A valid configuration reaches storage. The nil repository then stops Init.
			_, err = handlers.Initialize(t.Context(), app, []handlers.Descriptor{{Name: "site", New: func() handlers.Handler { return module }}}, nil)
			if err == nil || opened != (test.capacity > 0) {
				t.Fatalf("opened=%v error=%v", opened, err)
			}
			if test.capacity > 0 {
				got, err := module.settings.RateLimit.ClientCapacity()
				if err != nil || got != test.capacity {
					t.Fatalf("capacity=%d error=%v", got, err)
				}
			}
			other := Module(nil, nil).New().(*SiteModule)
			if other.settings.RateLimit != nil {
				t.Fatal("module instances share settings")
			}
		})
	}
}
