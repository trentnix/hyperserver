// Command web runs HyperServer's loopback-only development application.
// It imports the sample site and email authentication provider. It is not suitable
// for public deployment.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/trentnix/hyperserver/cmd/internal/setup"
	"github.com/trentnix/hyperserver/config"
)

func main() {
	l := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	info := buildInfo()
	l.Info("Application starting", "version", info.Version, "commit", info.Commit, "modified", info.Modified)
	contactDirectory := flag.String("contact-directory", "", "store contact submissions as JSON files in this directory instead of SQLite")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		stop()
		fmt.Println("Shutting down. Send another interrupt to force exit.")
	}()
	cfg, err := config.GetConfig()
	if err != nil {
		l.Error("Configuration failed", "error", err)
		os.Exit(1)
	}
	app, err := newApplication(cfg)
	if err != nil {
		l.Error("Application configuration failed", "error", err)
		os.Exit(1)
	}
	accounts := setup.NewAccountStorage(app.Config)
	if err := run(ctx, app, accounts, *contactDirectory); err != nil {
		l.Error("Application lifecycle failed", "error", redactLifecycleError(err, app))
		os.Exit(1)
	}
	l.Info("Application stopped")
}

// run delegates serving and resource ownership to the shared application lifecycle.
func run(ctx context.Context, s *application, accounts *setup.AccountStorage, contactDirectory string) error {
	return s.Run(ctx, func(ctx context.Context) (*http.Server, net.Listener, error) {
		return prepareHTTP(ctx, s, accounts, contactDirectory)
	})
}
