package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/trentnix/hyperserver/config"
)

func TestOwnedProviderFailureCleanup(t *testing.T) {
	for _, mode := range []string{"unknown option", "wrong driver", "canceled", "initialization failure"} {
		t.Run(mode, func(t *testing.T) {
			directory := t.TempDir()
			cfg := config.Config{App: config.AppConfig{WorkingDirectory: directory}, Database: config.DatabaseConfig{Driver: "sqlite3", Connection: "accounts.db"}}
			options := map[string]string{}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch mode {
			case "unknown option":
				options["unexpected"] = "private-value"
			case "wrong driver":
				cfg.Database.Driver = "postgres"
			case "canceled":
				cancel()
			case "initialization failure":
				cfg.Database.Connection = filepath.Join("missing", "accounts.db")
			}
			provider := OwnedProvider(cfg)
			repository, cleanup, err := provider.Open(ctx, options)
			if err == nil {
				t.Fatal("invalid setup succeeded")
			}
			if mode == "initialization failure" {
				if repository == nil || cleanup == nil {
					t.Fatal("lost cleanup for failed initialization")
				}
				if err := cleanup(); err != nil {
					t.Fatal(err)
				}
				if err := repository.(*AccountRepository).db.PingContext(t.Context()); err == nil || err.Error() != "sql: database is closed" {
					t.Fatal("pool was not closed", err)
				}
			} else if repository != nil || cleanup != nil {
				t.Fatal("invalid configuration acquired resources")
			}
			if mode == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatal("lost cancellation", err)
			}
			entries, err := os.ReadDir(directory)
			if err != nil || len(entries) != 0 {
				t.Fatalf("failure wrote files: %v %v", entries, err)
			}
		})
	}
}
