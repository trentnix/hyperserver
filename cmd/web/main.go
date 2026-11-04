// Command web runs the blog configured by config.yaml and modules/blog/config.yaml.
package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/internal/maintenance"
	"github.com/trentnix/hyperserver/pkg/application"
	"github.com/trentnix/hyperserver/pkg/health"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/logger"
)

// main connects process signals to graceful shutdown and reports startup failures.
func main() {
	l := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(logger.Set(context.Background(), l), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		l.Error("Blog stopped", "error", err)
		os.Exit(1)
	}
}

// run loads configuration and serves the blog until cancellation or a server error.
// The application owns shutdown of the HTTP server and its services.
func run(ctx context.Context) error {
	cfg, err := config.GetConfig()
	if err != nil {
		return err
	}
	lock, err := maintenance.Lock(cfg.App.WorkingDirectory)
	if err != nil {
		return err
	}
	defer lock.Close()
	app, err := newApplication(ctx, cfg)
	if err != nil {
		return err
	}
	return app.Run(ctx, func(ctx context.Context) (*http.Server, net.Listener, error) {
		httpServer, err := newHTTPServer(ctx, app)
		if err != nil {
			return nil, nil, err
		}
		listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", httpServer.Addr)
		if err == nil {
			logger.Get(ctx).Info("Blog listening", "address", listener.Addr().String())
		}
		return httpServer, listener, err
	})
}

func newHTTPServer(ctx context.Context, app *application.Application) (*http.Server, error) {
	var checks []health.Check
	if app.Config.Auth.RegistrationEnabled && app.Config.Auth.RegisterRequiresVerification && app.Mail != nil {
		checks = append(checks, health.Check{Name: "mail", Run: app.Mail.CheckHealth})
	}
	handler, err := app.HealthHandler(ctx, 2*time.Second, checks...)
	if err != nil {
		return nil, err
	}
	return server.NewHTTPServer(app.Config.HTTP, handler)
}
