// Command cleanup removes one bounded batch of expired sessions and account
// tokens from configured storage. It does not start HTTP or mail services.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/database"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func main() {
	batchSize := flag.Int("batch-size", 100, "maximum expired rows to delete from each store")
	timeout := flag.Duration("timeout", 10*time.Second, "deadline for this cleanup run")
	flag.Parse()
	if *batchSize < 1 || *timeout <= 0 {
		log.Fatal("batch-size and timeout must be positive")
	}
	cfg, err := config.GetConfig()
	if err != nil {
		log.Fatal(err)
	}
	if err := session.ValidateConfig(&cfg); err != nil {
		log.Fatal(err)
	}
	if cfg.App.WorkingDirectory != "" {
		if err := os.Chdir(cfg.App.WorkingDirectory); err != nil {
			log.Fatal(err)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	sessions, tokens, err := cleanup(ctx, &cfg, *batchSize)
	fmt.Printf("Deleted %d expired sessions and %d expired account tokens.\n", sessions, tokens)
	if err != nil {
		log.Fatal(err)
	}
}

func cleanup(ctx context.Context, cfg *config.Config, limit int) (sessions, tokens int64, err error) {
	if limit < 1 {
		return 0, 0, fmt.Errorf("cleanup batch size must be positive")
	}
	if cfg.Auth.Enabled {
		settings := cfg.Auth.AccountStorage
		switch {
		case settings.Provider != "" && settings.Provider != "sqlite":
			err = fmt.Errorf("account-token cleanup supports only the sqlite account provider")
		case len(settings.Options) != 0:
			err = fmt.Errorf("auth.accountStorage.options: sqlite uses database configuration and accepts no options")
		default:
			tokens, err = cleanupTokens(ctx, cfg.Database, limit)
		}
	}
	for _, provider := range cfg.HTTP.Session.Types {
		if !strings.EqualFold(provider, "sqliteStore") {
			continue
		}
		for name, options := range cfg.HTTP.Session.Stores {
			if strings.EqualFold(name, provider) {
				var sessionErr error
				sessions, sessionErr = cleanupSessions(ctx, options, limit)
				return sessions, tokens, errors.Join(err, sessionErr)
			}
		}
		return sessions, tokens, errors.Join(err, fmt.Errorf("selected SQLite session store is not configured"))
	}
	return sessions, tokens, err
}

func cleanupTokens(ctx context.Context, cfg config.DatabaseConfig, limit int) (count int64, err error) {
	db, err := database.Setup(cfg.Driver, cfg.Connection)
	if err != nil {
		return 0, err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	count, err = user.DeleteExpiredTokens(ctx, db, limit)
	if err != nil {
		err = fmt.Errorf("clean up account tokens: %w", err)
	}
	return
}

func cleanupSessions(ctx context.Context, options map[string]string, limit int) (count int64, err error) {
	db, err := database.Setup("sqlite3", options["connection"])
	if err != nil {
		return 0, err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	count, err = session.DeleteExpiredSQLiteSessions(ctx, db, options["sessiontable"], limit)
	if err != nil {
		err = fmt.Errorf("clean up sessions: %w", err)
	}
	return
}
