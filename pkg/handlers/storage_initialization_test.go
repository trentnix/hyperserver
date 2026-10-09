package handlers_test

import (
	"context"
	"errors"
	"testing"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/modules/site/models"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestInitializeStorageBeforeConsumersUsingSharedPool(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "ready", true: "schema failure"}[failed], func(t *testing.T) {
			app, err := server.NewApplicationServer(config.Config{Database: config.DatabaseConfig{Driver: "sqlite3", Connection: ":memory:"}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := app.Shutdown(); err != nil {
					t.Error(err)
				}
			})
			if err := app.InitializeDatabase(t.Context()); err != nil {
				t.Fatal(err)
			}
			app.Database.SetMaxOpenConns(1)
			if failed {
				// A conflicting index prevents account table creation.
				if _, err := app.Database.Exec(`CREATE TABLE unrelated (id INTEGER); CREATE INDEX user ON unrelated(id)`); err != nil {
					t.Fatal(err)
				}
			}
			var accounts user.AccountRepository
			var contacts *models.ContactRepository
			var setupErr error
			provider := dependencyDescriptor("accounts", &dependencyModule{
				init: func(ctx context.Context) error {
					if _, ok := ctx.Deadline(); !ok {
						t.Error("storage setup has no deadline")
					}
					repository := user.NewSQLiteAccountRepository(app.Database)
					setupErr = repository.Initialize(ctx)
					if setupErr != nil {
						return setupErr
					}
					accounts = repository
					return nil
				},
				provide: func() map[string]any { return map[string]any{"accounts": accounts} },
			})
			provider.Provides = []string{"accounts"}
			consumer := dependencyDescriptor("contacts", &dependencyModule{
				bind: func(d handlers.Dependencies) error {
					if failed {
						t.Error("consumer bound after failed storage setup")
					}
					bound, err := handlers.GetDependency[user.AccountRepository](d, "accounts")
					if err != nil {
						return err
					}
					if bound != accounts {
						t.Error("consumer received another account repository")
					}
					return nil
				},
				init: func(ctx context.Context) error {
					if failed {
						t.Error("consumer initialized after failed storage setup")
					}
					// The consumer can build its own repository on the same borrowed pool.
					var err error
					contacts, err = models.NewContactRepository(ctx, app.Database)
					return err
				},
			})
			consumer.Requires = []handlers.Requirement{{Capability: "accounts"}}
			instances, err := handlers.Initialize(t.Context(), app, []handlers.Descriptor{consumer, provider}, nil)
			if failed {
				if err == nil || setupErr == nil || !errors.Is(err, setupErr) || len(instances) != 0 || contacts != nil {
					t.Fatalf("failed startup: instances=%v error=%v storage error=%v", instances, err, setupErr)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if len(instances) != 2 || instances[0].Name != "accounts" {
					t.Fatalf("initialization order = %v", instances)
				}
				// Use fresh operation contexts after the initialization context is canceled.
				account := &user.User{Email: "person@example.invalid"}
				if err := accounts.Create(t.Context(), account); err != nil {
					t.Fatal(err)
				}
				if err := contacts.Create(t.Context(), &models.ContactSubmission{Email: account.Email}); err != nil {
					t.Fatal(err)
				}
				if _, err := accounts.GetByID(t.Context(), account.ID); err != nil {
					t.Fatal("consumer schema setup affected accounts:", err)
				}
			}
			if err := app.Database.PingContext(t.Context()); err != nil {
				t.Fatal("module initialization closed the borrowed pool:", err)
			}
			if err := app.Shutdown(); err != nil {
				t.Fatal(err)
			}
			if err := app.Database.PingContext(t.Context()); err == nil {
				t.Fatal("application shutdown left its pool open")
			}
		})
	}
}
