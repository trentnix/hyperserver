// Command web runs HyperServer's loopback-only development application.
// It imports the sample site and email authentication provider. It is not suitable
// for public deployment.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/trentnix/hyperserver/config"
	framework "github.com/trentnix/hyperserver/pkg/application"
	"github.com/trentnix/hyperserver/pkg/services/logger"
)

func main() {
	l := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	info := buildInfo()
	l.Info("Application starting", "version", info.Version, "commit", info.Commit, "modified", info.Modified)
	contactDirectory := flag.String("contact-directory", "", "store contact submissions as JSON files in this directory instead of SQLite")
	flag.Parse()

	ctx, stop := signal.NotifyContext(logger.Set(context.Background(), l), os.Interrupt, syscall.SIGTERM)
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
	app, err := newApplication(ctx, cfg, *contactDirectory, framework.Options{})
	if err != nil {
		l.Error("Application startup failed", "error", redactLifecycleError(err, &cfg))
		os.Exit(1)
	}
	if err := run(ctx, app); err != nil {
		l.Error("Application lifecycle failed", "error", redactLifecycleError(err, app.Config))
		os.Exit(1)
	}
	l.Info("Application stopped")
}
