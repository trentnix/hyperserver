package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/trentnix/hyperserver/pkg/util"
)

func TestStartupStoragePaths(t *testing.T) {
	for _, mode := range []string{"relative-database", "invalid-working-directory"} {
		t.Run(mode, func(t *testing.T) {
			working := t.TempDir()
			if mode == "invalid-working-directory" {
				working = filepath.Join(working, "missing")
			}
			yaml := strings.Replace(startupTestConfig, `connection: ":memory:"`, `connection: "relative.db"`, 1)
			yaml += fmt.Sprintf("app:\n  workingDirectory: %q\n", working)
			output, err := runStartupProcess(t, yaml, mode)
			if err != nil || !strings.Contains(string(output), "storage path checks passed") {
				t.Fatalf("startup storage paths: error=%v\n%s", err, output)
			}
		})
	}
}

// Run in a subprocess because startup changes the process working directory.
// Do not preinitialize services: the test must exercise run's actual ordering.
func checkStartupStoragePaths(t *testing.T) {
	t.Helper()
	launch, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.GetConfig()
	if err != nil {
		t.Fatal(err)
	}
	app, err := server.NewApplicationServer(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Inspect storage after initialization, then stop before module setup/listening.
	checked := errors.New("storage checked")
	app.Config.Auth.AccountStorage.Provider = "path-check"
	if err := app.RegisterAccountProvider("path-check", server.AccountProvider{
		Open: func(ctx context.Context, _ map[string]string) (user.AccountRepository, func() error, error) {
			first, err := app.Database.Conn(ctx)
			if err != nil {
				return nil, nil, err
			}
			defer first.Close()
			second, err := app.Database.Conn(ctx)
			if err != nil {
				return nil, nil, err
			}
			defer second.Close()
			want := filepath.Join(cfg.App.WorkingDirectory, "relative.db")
			for _, conn := range []*sql.Conn{first, second} {
				var sequence int
				var name, path string
				if err := conn.QueryRowContext(ctx, "PRAGMA database_list").Scan(&sequence, &name, &path); err != nil {
					return nil, nil, err
				}
				if path != want {
					return nil, nil, fmt.Errorf("database path=%q, want %q", path, want)
				}
			}
			if _, err := first.ExecContext(ctx, "CREATE TABLE path_check (id INTEGER)"); err != nil {
				return nil, nil, err
			}
			if _, err := second.ExecContext(ctx, "INSERT INTO path_check VALUES (1)"); err != nil {
				return nil, nil, err
			}
			return nil, nil, checked
		},
	}); err != nil {
		t.Fatal(err)
	}
	err = run(context.Background(), app, "")
	if os.Getenv("HS_STARTUP_TEST_HELPER") == "invalid-working-directory" {
		var directoryError *util.ErrFailedToSetWorkingDirectory
		if !errors.As(err, &directoryError) || app.Database != nil || app.Mail != nil || app.SessionManager != nil {
			t.Fatalf("invalid working directory acquired services or returned the wrong error: %v", err)
		}
	} else {
		if !errors.Is(err, checked) {
			t.Fatalf("storage check failed: %v", err)
		}
		if err := app.Database.Ping(); err == nil {
			t.Fatal("startup failure left the pool open")
		}
	}
	if _, err := os.Stat(filepath.Join(launch, "relative.db")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("startup created a database in the launch directory: %v", err)
	}
	fmt.Println("storage path checks passed")
}
