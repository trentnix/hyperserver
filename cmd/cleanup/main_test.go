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
		})
	}
}

func TestCleanupDisabledAndUnselectedStores(t *testing.T) {
	cfg, accounts, sessions := cleanupFixture(t)
	cfg.Auth.Enabled = false
	cfg.HTTP.Session.Types = map[string]string{"default": "cookieStore"}
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

func TestCleanupCommand(t *testing.T) {
	if os.Getenv("HYPERSERVER_CLEANUP_HELPER") == "1" {
		flag.CommandLine = flag.NewFlagSet("cleanup", flag.ExitOnError)
		os.Args = []string{"cleanup", "-batch-size", "1"}
		main()
		return
	}
	cfg, _, sessions := cleanupFixture(t)
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
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestCleanupCommand$")
	cmd.Dir = dir
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "HYPERSERVER_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "HYPERSERVER_CLEANUP_HELPER=1")
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "Deleted 1 expired sessions and 0 expired account tokens.") {
		t.Fatalf("command: %v\n%s", err, out)
	}
	var count int
	if err := sessions.Get(&count, `SELECT count(*) FROM sessions`); err != nil || count != 3 {
		t.Fatalf("command deleted wrong rows: %d, %v", count, err)
	}
}
