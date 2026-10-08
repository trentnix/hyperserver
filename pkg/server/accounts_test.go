package server

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/database"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestInitializeAccountsDisabled(t *testing.T) {
	s := &ApplicationServer{Config: &config.Config{}}
	if err := s.InitializeAccounts(context.Background()); err != nil {
		t.Fatal("disabled auth required a database:", err)
	}

	s.Database = accountSetupTestDB(t)
	if err := s.InitializeAccounts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.AccountRepository != nil {
		t.Fatal("disabled auth created a repository")
	}
	var count int
	if err := s.Database.Get(&count, `SELECT count(*) FROM sqlite_master WHERE type = 'table'`); err != nil || count != 0 {
		t.Fatalf("disabled auth created tables: count = %d, error = %v", count, err)
	}
}

func TestInitializeAccountsDeadline(t *testing.T) {
	for _, timeout := range []time.Duration{accountSetupTimeout, time.Second} {
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
				if timeout < accountSetupTimeout {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, timeout)
					defer cancel()
				}
				s := &ApplicationServer{Config: &config.Config{Auth: config.AuthConfig{Enabled: true}}, Database: db}
				start := time.Now()
				if err := s.InitializeAccounts(ctx); !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("setup deadline error = %v", err)
				}
				if elapsed := time.Since(start); elapsed != timeout {
					t.Fatalf("setup waited %s, want %s", elapsed, timeout)
				}
				if s.AccountRepository != nil {
					t.Fatal("failed setup published a repository")
				}
				if err := conn.Close(); err != nil {
					t.Fatal(err)
				}
				if err := s.InitializeAccounts(context.Background()); err != nil {
					t.Fatal("retry after deadline:", err)
				}
				assertAccountRepositoryReady(t, s.AccountRepository)
			})
		})
	}
}

func TestInitializeAccountsPreservesSuppliedRepository(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		repository := &suppliedAccountRepository{}
		s := &ApplicationServer{
			Config:            &config.Config{Auth: config.AuthConfig{Enabled: enabled}},
			AccountRepository: repository,
		}
		if err := s.InitializeAccounts(context.Background()); err != nil {
			t.Fatal("supplied repository required a database:", err)
		}
		if s.AccountRepository != repository {
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
			s := &ApplicationServer{Config: &config.Config{Auth: config.AuthConfig{Enabled: true}}, Database: db}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch scenario {
			case "missing pool":
				s.Database = nil
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

			err := s.InitializeAccounts(ctx)
			if err == nil || s.AccountRepository != nil {
				t.Fatalf("failed setup: error = %v, repository = %v", err, s.AccountRepository)
			}
			var unavailable *database.ErrDatabaseUnavailable
			if scenario == "missing pool" && !errors.As(err, &unavailable) {
				t.Fatalf("missing pool error = %v", err)
			}
			if scenario == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation error = %v", err)
			}

			if scenario == "closed pool" {
				db = accountSetupTestDB(t)
			}
			s.Database = db
			var count int
			if err := db.Get(&count, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name IN ('user', 'usertoken')`); err != nil || count != 0 {
				t.Fatalf("failed setup left account tables or closed the pool: count = %d, error = %v", count, err)
			}
			if scenario == "schema conflict" {
				if _, err := db.Exec(`DROP INDEX usertoken`); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.InitializeAccounts(context.Background()); err != nil {
				t.Fatal("retry after failure:", err)
			}
			assertAccountRepositoryReady(t, s.AccountRepository)
		})
	}
}

func TestApplicationOwnsAccountPool(t *testing.T) {
	first := &ApplicationServer{Config: &config.Config{Auth: config.AuthConfig{Enabled: true}}, Database: accountSetupTestDB(t)}
	second := &ApplicationServer{Config: first.Config, Database: accountSetupTestDB(t)}
	for _, s := range []*ApplicationServer{first, second} {
		if err := s.InitializeAccounts(context.Background()); err != nil {
			t.Fatal(err)
		}
		assertAccountRepositoryReady(t, s.AccountRepository)
	}
	repository := first.AccountRepository
	if err := first.InitializeAccounts(context.Background()); err != nil || first.AccountRepository != repository {
		t.Fatal("repeated initialization replaced the repository:", err)
	}
	if err := first.Database.Ping(); err != nil {
		t.Fatal("initialization closed the shared pool:", err)
	}
	if err := first.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if err := first.Database.Ping(); err == nil {
		t.Fatal("shutdown left the shared pool open")
	}
	if _, err := repository.GetByEmail(context.Background(), "ready@example.invalid"); err == nil {
		t.Fatal("repository remained usable after its pool closed")
	}
	if _, err := second.AccountRepository.GetByEmail(context.Background(), "ready@example.invalid"); err != nil {
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
	if err := repository.CreateToken(ctx, metadata); err != nil {
		t.Fatal("token storage not ready:", err)
	}
	if _, err := repository.GetToken(ctx, metadata.TokenHash, metadata.Purpose); err != nil {
		t.Fatal("token lookup not ready:", err)
	}
}

func accountSetupTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db, err := sqlx.Open("sqlite3", filepath.Join(t.TempDir(), "accounts.db"))
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
