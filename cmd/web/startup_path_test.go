package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/config"
	framework "github.com/trentnix/hyperserver/pkg/application"
)

func TestStartupOutsideRepository(t *testing.T) {
	// The subprocess receives only config.yaml in a new directory. No repository
	// assets or workingDirectory override are available to the application.
	output, err := runStartupProcess(t, startupTestConfig, "initialize")
	if err != nil || !strings.Contains(string(output), "startup initialization passed") {
		t.Fatalf("startup outside repository: error=%v\n%s", err, output)
	}
}

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

// Run in a subprocess to isolate launch directories and configuration loading.
// Exercise the same constructor as the command, without changing the process directory.
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

	app, err := newApplication(t.Context(), cfg, "", framework.Options{})
	if os.Getenv("HS_STARTUP_TEST_HELPER") == "invalid-working-directory" {
		if err == nil || !strings.Contains(err.Error(), "app.workingDirectory") || app != nil {
			t.Fatalf("invalid working directory: app=%v err=%v", app, err)
		}
	} else {
		if err != nil {
			t.Fatal(err)
		}
		defer app.Shutdown()
		app.contactDB.SetMaxOpenConns(2)
		first, err := app.contactDB.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer first.Close()
		second, err := app.contactDB.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer second.Close()
		want := filepath.Join(cfg.App.WorkingDirectory, "relative.db")
		for _, conn := range []*sql.Conn{first, second} {
			var sequence int
			var name, path string
			if err := conn.QueryRowContext(t.Context(), "PRAGMA database_list").Scan(&sequence, &name, &path); err != nil {
				t.Fatal(err)
			}
			if path != want {
				t.Fatalf("database path=%q, want %q", path, want)
			}
		}
		if _, err := first.ExecContext(t.Context(), "CREATE TABLE path_check (id INTEGER)"); err != nil {
			t.Fatal(err)
		}
		if _, err := second.ExecContext(t.Context(), "INSERT INTO path_check VALUES (1)"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(launch, "relative.db")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("startup created a database in the launch directory: %v", err)
	}
	if current, err := os.Getwd(); err != nil || current != launch {
		t.Fatalf("startup changed process directory: %q, %v", current, err)
	}
	fmt.Println("storage path checks passed")
}
