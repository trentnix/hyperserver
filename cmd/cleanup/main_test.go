package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/database"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func cleanupFixture(t *testing.T) (*config.Config, *sqlx.DB, *sqlx.DB) {
	t.Helper()
	dir := t.TempDir()
	accountPath, sessionPath := filepath.Join(dir, "accounts.db"), filepath.Join(dir, "sessions.db")
	open := func(path string) *sqlx.DB {
		db, err := database.Setup("sqlite3", path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		return db
	}
	accounts, sessions := open(accountPath), open(sessionPath)
	if err := user.PrepareDatabase(context.Background(), accounts); err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.Exec(`CREATE TABLE sessions (id TEXT PRIMARY KEY, expires_at TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	for i := range 4 {
		expires := time.Now().Add(-time.Hour)
		if i == 3 {
			expires = time.Now().Add(time.Hour)
		}
		if _, err := sessions.Exec(`INSERT INTO sessions VALUES (?, ?)`, fmt.Sprint(i), expires); err != nil {
			t.Fatal(err)
		}
		if _, err := accounts.Exec(`INSERT INTO usertoken (user_id, token_hash, token_type, expires_at) VALUES ('account', ?, 'auth-reset', ?)`, fmt.Sprint(i), expires); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Config{Database: config.DatabaseConfig{Driver: "sqlite3", Connection: accountPath}}
	cfg.Auth.Enabled = true
	cfg.HTTP.Session.Types = map[string]string{"default": "SQLiteStore", "auth": "sqliteStore"}
	cfg.HTTP.Session.Stores = map[string]map[string]string{"sqliteStore": {"enabled": "true", "connection": sessionPath, "sessiontable": "sessions"}}
	return cfg, accounts, sessions
}

func TestCleanupSeparateStores(t *testing.T) {
	cfg, accounts, sessions := cleanupFixture(t)
	for _, want := range []int64{2, 1, 0} {
		s, a, err := cleanup(context.Background(), cfg, 2)
		if err != nil || s != want || a != want {
			t.Fatalf("cleanup = (%d, %d, %v), want (%d, %d, nil)", s, a, err, want, want)
		}
	}
	for db, query := range map[*sqlx.DB]string{accounts: "SELECT count(*) FROM usertoken", sessions: "SELECT count(*) FROM sessions"} {
		var count int
		if err := db.Get(&count, query); err != nil || count != 1 {
			t.Fatalf("live rows = %d, %v, want 1", count, err)
		}
	}
}

func TestCleanupRejectsUnsupportedAccountStorage(t *testing.T) {
	for _, scenario := range []string{"custom provider", "SQLite options"} {
		t.Run(scenario, func(t *testing.T) {
			cfg, accounts, _ := cleanupFixture(t)
			if scenario == "custom provider" {
				cfg.Auth.AccountStorage.Provider = "custom"
			} else {
				cfg.Auth.AccountStorage.Options = map[string]string{"connection": "another-database"}
			}
			sessions, tokens, err := cleanup(context.Background(), cfg, 2)
			if err == nil || sessions != 2 || tokens != 0 {
				t.Fatalf("unsupported account cleanup = (%d, %d, %v)", sessions, tokens, err)
			}
			var count int
			if err := accounts.Get(&count, "SELECT count(*) FROM usertoken"); err != nil || count != 4 {
				t.Fatalf("unsupported account cleanup changed SQLite tokens: count=%d, error=%v", count, err)
			}
		})
	}
}

func TestCleanupPartialFailure(t *testing.T) {
	for _, failed := range []string{"accounts", "sessions"} {
		t.Run(failed, func(t *testing.T) {
			cfg, accounts, sessions := cleanupFixture(t)
			wantSessions, wantTokens := int64(2), int64(2)
			if failed == "accounts" {
				accounts.MustExec(`DROP TABLE usertoken`)
				wantTokens = 0
			} else {
				sessions.MustExec(`DROP TABLE sessions`)
				wantSessions = 0
			}
			s, a, err := cleanup(context.Background(), cfg, 2)
			if err == nil || s != wantSessions || a != wantTokens {
				t.Fatalf("partial cleanup = (%d, %d, %v)", s, a, err)
			}
			db, table := accounts, "usertoken"
			if failed == "sessions" {
				db, table = sessions, "sessions"
			}
			var count int
			if err := db.Get(&count, "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table); err != nil || count != 0 {
				t.Fatalf("cleanup recreated missing schema: count=%d, error=%v", count, err)
			}
		})
	}
}

func TestCleanupDisabledAndUnselectedStores(t *testing.T) {
	cfg, accounts, sessions := cleanupFixture(t)
	cfg.Auth.Enabled = false
	cfg.HTTP.Session.Types = map[string]string{"default": "cookieStore"}
	cfg.HTTP.Session.Stores["cookieStore"] = map[string]string{"enabled": "true"}
	s, a, err := cleanup(context.Background(), cfg, 2)
	if err != nil || s != 0 || a != 0 {
		t.Fatalf("inactive cleanup = (%d, %d, %v)", s, a, err)
	}
	for db, query := range map[*sqlx.DB]string{accounts: "SELECT count(*) FROM usertoken", sessions: "SELECT count(*) FROM sessions"} {
		var count int
		if err := db.Get(&count, query); err != nil || count != 4 {
			t.Fatalf("inactive storage changed: %d, %v", count, err)
		}
	}
}

func TestCleanupInvalidLimitAndCancellation(t *testing.T) {
	cfg, _, _ := cleanupFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, limit := range []int{0, -1, 2} {
		s, a, err := cleanup(ctx, cfg, limit)
		if err == nil || s != 0 || a != 0 {
			t.Fatalf("failed cleanup = (%d, %d, %v)", s, a, err)
		}
		if limit > 0 && !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation lost: %v", err)
		}
	}
}

func TestCleanupSessionSelection(t *testing.T) {
	for _, scenario := range []string{"unknown", "missing", "disabled", "invalid enabled", "duplicate", "missing table option", "missing connection", "cookie only", "unselected invalid store"} {
		t.Run(scenario, func(t *testing.T) {
			cfg, _, sessionsDB := cleanupFixture(t)
			cfg.Auth.Enabled = false
			options := cfg.HTTP.Session.Stores["sqliteStore"]
			switch scenario {
			case "unknown":
				cfg.HTTP.Session.Types = map[string]string{"default": "custom"}
				cfg.HTTP.Session.Stores["custom"] = map[string]string{"enabled": "true"}
			case "missing":
				delete(cfg.HTTP.Session.Stores, "sqliteStore")
			case "disabled":
				options["enabled"] = "false"
			case "invalid enabled":
				options["enabled"] = "perhaps"
			case "duplicate":
				cfg.HTTP.Session.Stores["SQLITESTORE"] = options
			case "missing table option":
				delete(options, "sessiontable")
			case "missing connection":
				delete(options, "connection")
			case "cookie only":
				cfg.HTTP.Session.Types = map[string]string{"default": "cookieStore"}
				cfg.HTTP.Session.Stores["cookieStore"] = map[string]string{"enabled": "true"}
			case "unselected invalid store":
				cfg.HTTP.Session.Stores["unused"] = map[string]string{"enabled": "perhaps"}
			}
			sessions, tokens, err := cleanup(context.Background(), cfg, 2)
			wantErr := scenario != "cookie only" && scenario != "unselected invalid store"
			var wantDeleted int64
			if scenario == "unselected invalid store" {
				wantDeleted = 2
			}
			if (err != nil) != wantErr || sessions != wantDeleted || tokens != 0 {
				t.Fatalf("selection result: sessions=%d, tokens=%d, error=%v", sessions, tokens, err)
			}
			var remaining int64
			if err := sessionsDB.Get(&remaining, "SELECT count(*) FROM sessions"); err != nil || remaining != 4-wantDeleted {
				t.Fatalf("unexpected session changes: count=%d, error=%v", remaining, err)
			}
		})
	}
}

func TestCleanupContinuesAfterSelectedSessionProviderFailure(t *testing.T) {
	for _, name := range []string{"aaa-unknown", "zzz-unknown"} {
		t.Run(name, func(t *testing.T) {
			cfg, _, sessionsDB := cleanupFixture(t)
			cfg.HTTP.Session.Types["unsupported"] = name
			cfg.HTTP.Session.Stores[name] = map[string]string{"enabled": "true"}
			sessions, tokens, err := cleanup(context.Background(), cfg, 2)
			if err == nil || !strings.Contains(err.Error(), name) || sessions != 2 || tokens != 2 {
				t.Fatalf("independent cleanup: sessions=%d, tokens=%d, error=%v", sessions, tokens, err)
			}
			var remaining int
			if err := sessionsDB.Get(&remaining, "SELECT count(*) FROM sessions"); err != nil || remaining != 2 {
				t.Fatalf("selected store skipped or cleaned twice: count=%d, error=%v", remaining, err)
			}
		})
	}
}

func TestCleanupReportsBothFailures(t *testing.T) {
	cfg, accounts, sessions := cleanupFixture(t)
	if _, err := accounts.Exec("DROP TABLE usertoken"); err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.Exec("DROP TABLE sessions"); err != nil {
		t.Fatal(err)
	}
	s, a, err := cleanup(context.Background(), cfg, 2)
	if s != 0 || a != 0 || err == nil || !strings.Contains(err.Error(), "usertoken") || !strings.Contains(err.Error(), "sessions") {
		t.Fatalf("combined failures: sessions=%d, tokens=%d, error=%v", s, a, err)
	}
}

func TestCleanupCommand(t *testing.T) {
	if os.Getenv("HYPERSERVER_CLEANUP_HELPER") == "1" {
		flag.CommandLine = flag.NewFlagSet("cleanup", flag.ExitOnError)
		os.Args = []string{"cleanup", "-batch-size", "1"}
		main()
		return
	}
	for _, scenario := range []string{"success", "partial failure"} {
		t.Run(scenario, func(t *testing.T) {
			cfg, accounts, sessions := cleanupFixture(t)
			dir := t.TempDir()
			// Relative database paths follow app.workingDirectory, not the config directory.
			yaml := fmt.Sprintf(`app:
  workingDirectory: %q
http:
  session:
    jwtKey: cleanup-test-key
    tokenAge: 1h
    cookieAge: 1h
    types:
      default: sqliteStore
    stores:
      sqliteStore:
        enabled: true
        connection: sessions.db
        sessionTable: sessions
`, filepath.Dir(cfg.Database.Connection))
			if scenario == "partial failure" {
				yaml += `auth:
  enabled: true
  jwtKey: cleanup-account-test-key
  verificationTokenExpiration: 1h
  resetTokenExpiration: 1h
  accountStorage:
    provider: unknown
`
			}
			if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCleanupCommand$")
			cmd.Dir = dir
			for _, entry := range os.Environ() {
				if !strings.HasPrefix(entry, "HYPERSERVER_") {
					cmd.Env = append(cmd.Env, entry)
				}
			}
			cmd.Env = append(cmd.Env, "HYPERSERVER_CLEANUP_HELPER=1")
			out, err := cmd.CombinedOutput()
			if !strings.Contains(string(out), "Deleted 1 expired sessions and 0 expired account tokens.") {
				t.Fatalf("command: %v\n%s", err, out)
			}
			if scenario == "success" && err != nil {
				t.Fatalf("successful cleanup failed: %v\n%s", err, out)
			}
			if scenario == "partial failure" {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 || !strings.Contains(string(out), "unknown provider") {
					t.Fatalf("partial failure was not reported with exit 1: %v\n%s", err, out)
				}
			}
			var count int
			if err := sessions.Get(&count, `SELECT count(*) FROM sessions`); err != nil || count != 3 {
				t.Fatalf("command deleted wrong rows: %d, %v", count, err)
			}
			if err := accounts.Get(&count, "SELECT count(*) FROM usertoken"); err != nil || count != 4 {
				t.Fatalf("command touched unselected account storage: count=%d, error=%v", count, err)
			}
		})
	}
}
