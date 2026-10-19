package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
	dbsqlite "github.com/trentnix/hyperserver/pkg/database/sqlite"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestOwnedProviderFailureCleanup(t *testing.T) {
	for _, mode := range []string{"unknown option", "missing connection", "blank connection", "invalid connection", "canceled", "initialization failure"} {
		t.Run(mode, func(t *testing.T) {
			directory := t.TempDir()
			cfg := config.Config{App: config.AppConfig{WorkingDirectory: directory}, Database: config.DatabaseConfig{Driver: "sqlite3", Connection: "accounts.db"}}
			options := map[string]string{"connection": "accounts.db"}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch mode {
			case "unknown option":
				options["unexpected"] = "private-value"
			case "missing connection":
				delete(options, "connection")
			case "blank connection":
				options["connection"] = "   "
			case "invalid connection":
				options["connection"] = "file:private-value%zz"
			case "canceled":
				cancel()
			case "initialization failure":
				options["connection"] = filepath.Join("missing", "accounts.db")
			}
			provider := OwnedProvider(cfg)
			repository, cleanup, err := provider.Open(ctx, options)
			if err == nil {
				t.Fatal("invalid setup succeeded")
			}
			if strings.Contains(err.Error(), "private-value") {
				t.Fatal("configuration error exposed option values", err)
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

func TestConfiguredAccountsUseProviderConnection(t *testing.T) {
	for _, mode := range []string{"separate", "shared", "file URI", "absolute", "other platform"} {
		t.Run(mode, func(t *testing.T) {
			directory := t.TempDir()
			shared, err := dbsqlite.Open(filepath.Join(directory, "contacts.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { shared.Close() })
			if _, err := shared.ExecContext(t.Context(), "CREATE TABLE contact_marker (id INTEGER)"); err != nil {
				t.Fatal(err)
			}
			cfg := config.Config{
				App:      config.AppConfig{WorkingDirectory: directory},
				Database: config.DatabaseConfig{Driver: "sqlite3", Connection: "contacts.db"},
			}
			connection := "accounts.db"
			switch mode {
			case "shared":
				connection = "contacts.db"
			case "file URI":
				connection = "file:accounts.db"
			case "absolute":
				connection = filepath.Join(directory, "accounts.db")
			case "other platform":
				cfg.Database = config.DatabaseConfig{Driver: "postgres", Connection: "unused"}
			}
			options := map[string]string{"connection": connection + "?_timeout=37&_fk=true"}
			provider := OwnedProvider(cfg)
			repository, closeRepository, err := provider.Open(t.Context(), options)
			if closeRepository != nil {
				t.Cleanup(func() { closeRepository() })
			}
			if err != nil {
				t.Fatal(err)
			}
			var timeout, foreignKeys int
			pool := repository.(*AccountRepository).db
			if err := pool.QueryRowContext(t.Context(), "PRAGMA busy_timeout").Scan(&timeout); err != nil || timeout != 37 {
				t.Fatalf("connection options lost: timeout=%d err=%v", timeout, err)
			}
			if err := pool.QueryRowContext(t.Context(), "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil || foreignKeys != 1 {
				t.Fatalf("foreign keys disabled: %d err=%v", foreignKeys, err)
			}
			account := &user.User{Email: "reader@example.com", Password: "stored-hash", RegistrationAuthType: "email"}
			if err := repository.Create(t.Context(), account); err != nil {
				t.Fatal(err)
			}
			if err := repository.InsertToken(t.Context(), user.TokenMetadata{
				AccountID: account.ID, TokenHash: strings.Repeat("a", 64), Purpose: user.ResetTokenPurpose, ExpiresAt: time.Now().Add(-time.Hour),
			}); err != nil {
				t.Fatal(err)
			}
			if err := closeRepository(); err != nil {
				t.Fatal(err)
			}

			maintenance, closeMaintenance, err := provider.OpenMaintenance(t.Context(), options)
			if closeMaintenance != nil {
				t.Cleanup(func() { closeMaintenance() })
			}
			if err != nil {
				t.Fatal(err)
			}
			if count, err := maintenance.DeleteExpiredTokens(t.Context(), 1); err != nil || count != 1 {
				t.Fatalf("maintenance used different storage: count=%d err=%v", count, err)
			}
			var tables int
			if err := shared.QueryRowContext(t.Context(), "SELECT count(*) FROM sqlite_master WHERE name = 'user'").Scan(&tables); err != nil || (tables == 1) != (mode == "shared") {
				t.Fatalf("account schema in contact database: tables=%d err=%v", tables, err)
			}
			if _, err := shared.ExecContext(t.Context(), "INSERT INTO contact_marker VALUES (1)"); err != nil {
				t.Fatal("account setup or cleanup affected contacts", err)
			}
		})
	}
}

func TestConfiguredMaintenanceRejectsInvalidOptions(t *testing.T) {
	for _, options := range []map[string]string{
		nil, {"connection": ""}, {"connection": "   "}, {"connection": "file:private-value%zz"}, {"typo": "private-value"},
	} {
		cfg := config.Config{
			App:      config.AppConfig{WorkingDirectory: t.TempDir()},
			Database: config.DatabaseConfig{Driver: "sqlite3", Connection: "must-not-open.db"},
		}
		maintenance, closeStore, err := OwnedProvider(cfg).OpenMaintenance(t.Context(), options)
		if err == nil || maintenance != nil || closeStore != nil || !strings.Contains(err.Error(), "auth.accountStorage.options") || strings.Contains(err.Error(), "private-value") {
			t.Fatalf("invalid options returned resources or unsafe error: %v", err)
		}
		entries, err := os.ReadDir(cfg.App.WorkingDirectory)
		if err != nil || len(entries) != 0 {
			t.Fatalf("maintenance touched storage: %v %v", entries, err)
		}
	}
}
