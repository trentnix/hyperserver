package main

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/trentnix/hyperserver/cmd/internal/setup"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/database"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/user"
	usersqlite "github.com/trentnix/hyperserver/pkg/services/user/sqlite"
)

type accountSetupHarness struct {
	*setup.AccountStorage
	app *application
}

func newAccountSetup(app *application) *accountSetupHarness {
	return &accountSetupHarness{AccountStorage: setup.NewAccountStorage(app.Config), app: app}
}

func (s *accountSetupHarness) Initialize(ctx context.Context) error {
	return initializeAccounts(ctx, s.app, s.AccountStorage)
}

func TestInitializeAccountsDisabled(t *testing.T) {
	s := newAccountSetup(testApplicationWithPool(&server.ApplicationServer{Config: &config.Config{}}, nil))
	if err := s.Initialize(context.Background()); err != nil {
		t.Fatal("disabled auth required a database:", err)
	}

	s.app.ownDatabase(accountSetupTestDB(t))
	if err := s.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.app.AccountRepository != nil {
		t.Fatal("disabled auth created a repository")
	}
	var count int
	if err := s.app.Database.QueryRowContext(t.Context(), `SELECT count(*) FROM sqlite_master WHERE type = 'table'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("disabled auth created tables: count = %d, error = %v", count, err)
	}
}

func TestInitializeAccountsDeadline(t *testing.T) {
	for _, timeout := range []time.Duration{10 * time.Second, time.Second} {
		t.Run(timeout.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				db := accountSetupTestDB(t)
				// Hold the only connection so setup must wait for its deadline.
				conn, err := db.Conn(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				ctx := context.Background()
				if timeout < 10*time.Second {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, timeout)
					defer cancel()
				}
				s := newAccountSetup(testApplicationWithPool(&server.ApplicationServer{Config: &config.Config{Auth: config.AuthConfig{Enabled: true}}}, db))
				start := time.Now()
				if err := s.Initialize(ctx); !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("setup deadline error = %v", err)
				}
				if elapsed := time.Since(start); elapsed != timeout {
					t.Fatalf("setup waited %s, want %s", elapsed, timeout)
				}
				if s.app.AccountRepository != nil {
					t.Fatal("failed setup published a repository")
				}
				if err := conn.Close(); err != nil {
					t.Fatal(err)
				}
				if err := s.Initialize(context.Background()); err != nil {
					t.Fatal("retry after deadline:", err)
				}
				assertAccountRepositoryReady(t, s.app.AccountRepository)
			})
		})
	}
}

func TestInitializeAccountsPreservesSuppliedRepository(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		repository := &suppliedAccountRepository{}
		s := newAccountSetup(testApplicationWithPool(&server.ApplicationServer{
			Config: &config.Config{Auth: config.AuthConfig{Enabled: enabled}},
		}, nil))
		s.app.AccountRepository = repository
		if err := s.Initialize(context.Background()); err != nil {
			t.Fatal("supplied repository required a database:", err)
		}
		if s.app.AccountRepository != repository {
			t.Fatal("initialization replaced the supplied repository")
		}
	}
}

// No methods are needed: startup must leave a supplied repository alone.
type suppliedAccountRepository struct{ user.AccountRepository }

func TestInitializeAccountsFailureAndRetry(t *testing.T) {
	for _, scenario := range []string{"missing pool", "closed pool", "canceled", "schema conflict"} {
		t.Run(scenario, func(t *testing.T) {
			db := accountSetupTestDB(t)
			s := newAccountSetup(testApplicationWithPool(&server.ApplicationServer{Config: &config.Config{Auth: config.AuthConfig{Enabled: true}}}, db))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch scenario {
			case "missing pool":
				s.app.Database = nil
			case "closed pool":
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
			case "canceled":
				cancel()
			case "schema conflict":
				// Fail after the account table is created to check rollback.
				if _, err := db.Exec(`CREATE TABLE existing (id TEXT); CREATE INDEX usertoken ON existing(id)`); err != nil {
					t.Fatal(err)
				}
			}

			err := s.Initialize(ctx)
			if err == nil || s.app.AccountRepository != nil {
				t.Fatalf("failed setup: error = %v, repository = %v", err, s.app.AccountRepository)
			}
			if scenario == "missing pool" && !errors.Is(err, database.ErrDatabaseUnavailable) {
				t.Fatalf("missing pool error = %v", err)
			}
			if scenario == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation error = %v", err)
			}

			if scenario == "closed pool" {
				db = accountSetupTestDB(t)
			}
			s.app.Database = db
			var count int
			if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name IN ('user', 'usertoken')`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("failed setup left account tables or closed the pool: count = %d, error = %v", count, err)
			}
			if scenario == "schema conflict" {
				if _, err := db.Exec(`DROP INDEX usertoken`); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.Initialize(context.Background()); err != nil {
				t.Fatal("retry after failure:", err)
			}
			assertAccountRepositoryReady(t, s.app.AccountRepository)
		})
	}
}

func TestInitializeAccountsRejectsIncompatibleSchema(t *testing.T) {
	db := accountSetupTestDB(t)
	if err := usersqlite.NewAccountRepository(db).Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), "ALTER TABLE user RENAME COLUMN email_key TO legacy_email_key"); err != nil {
		t.Fatal(err)
	}
	s := newAccountSetup(testApplicationWithPool(&server.ApplicationServer{
		Config: &config.Config{Auth: config.AuthConfig{Enabled: true}},
	}, db))
	t.Cleanup(func() { s.app.Shutdown() })

	err := s.Initialize(t.Context())
	if err == nil || !strings.Contains(err.Error(), "email_key") || s.app.AccountRepository != nil {
		t.Fatalf("incompatible schema published a repository: %v, %v", s.app.AccountRepository, err)
	}
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatal("failed setup closed its borrowed pool:", err)
	}

	if _, err := db.ExecContext(t.Context(), "ALTER TABLE user RENAME COLUMN legacy_email_key TO email_key"); err != nil {
		t.Fatal(err)
	}
	if err := s.Initialize(t.Context()); err != nil {
		t.Fatal("retry after repair:", err)
	}
	assertAccountRepositoryReady(t, s.app.AccountRepository)
}

func TestApplicationOwnsAccountPool(t *testing.T) {
	first := newAccountSetup(testApplicationWithPool(&server.ApplicationServer{Config: &config.Config{Auth: config.AuthConfig{Enabled: true}}}, accountSetupTestDB(t)))
	second := newAccountSetup(testApplicationWithPool(&server.ApplicationServer{Config: first.app.Config}, accountSetupTestDB(t)))
	for _, s := range []*accountSetupHarness{first, second} {
		if err := s.Initialize(context.Background()); err != nil {
			t.Fatal(err)
		}
		assertAccountRepositoryReady(t, s.app.AccountRepository)
	}
	repository := first.app.AccountRepository
	if err := first.Initialize(context.Background()); err != nil || first.app.AccountRepository != repository {
		t.Fatal("repeated initialization replaced the repository:", err)
	}
	if err := first.app.Database.Ping(); err != nil {
		t.Fatal("initialization closed the shared pool:", err)
	}
	if err := first.app.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if err := first.app.Database.Ping(); err == nil {
		t.Fatal("shutdown left the shared pool open")
	}
	if _, err := repository.GetByEmail(context.Background(), "ready@example.invalid"); err == nil {
		t.Fatal("repository remained usable after its pool closed")
	}
	if _, err := second.app.AccountRepository.GetByEmail(context.Background(), "ready@example.invalid"); err != nil {
		t.Fatal("shutdown affected another application's repository:", err)
	}
}

func assertAccountRepositoryReady(t *testing.T, repository user.AccountRepository) {
	t.Helper()
	if repository == nil {
		t.Fatal("successful setup did not publish a repository")
	}
	ctx := context.Background()
	account := &user.User{Email: "ready@example.invalid"}
	if err := repository.Create(ctx, account); err != nil {
		t.Fatal("account storage not ready:", err)
	}
	if _, err := repository.GetByID(ctx, account.ID); err != nil {
		t.Fatal("account lookup not ready:", err)
	}
	metadata := user.TokenMetadata{AccountID: account.ID, TokenHash: "stored-hash", Purpose: "auth-reset", ExpiresAt: time.Now().Add(time.Hour)}
	if err := repository.InsertToken(ctx, metadata); err != nil {
		t.Fatal("token storage not ready:", err)
	}
	if _, err := repository.GetToken(ctx, metadata.TokenHash, metadata.Purpose); err != nil {
		t.Fatal("token lookup not ready:", err)
	}
}

func accountSetupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return db
}
