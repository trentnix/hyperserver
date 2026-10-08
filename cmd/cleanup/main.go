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
	"syscall"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/session"
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
	app := &server.ApplicationServer{Config: cfg}
	tokens, tokenErr := app.CleanupAccountTokens(ctx, limit)
	sessions, sessionErr := session.CleanupExpired(ctx, cfg, limit)
	return sessions, tokens, errors.Join(tokenErr, sessionErr)
}
