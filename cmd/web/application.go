package main

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/trentnix/hyperserver/config"
	framework "github.com/trentnix/hyperserver/pkg/application"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/health"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/logger"
)

// application adds the reference site's contact storage to the configured framework.
type application struct {
	*framework.Application
	contactDB     *sql.DB
	contactHealth func(context.Context) error
}

func newApplication(ctx context.Context, cfg config.Config, contactDirectory string, options framework.Options) (*application, error) {
	if _, err := referenceListenAddress(cfg.HTTP.ListenHost, cfg.HTTP.Port); err != nil {
		return nil, err
	}

	app := &application{}
	selectModules := options.Modules
	options.ContentSecurityPolicy = referenceContentSecurityPolicy
	options.Modules = func(ctx context.Context, configured *framework.Application) ([]handlers.Descriptor, error) {
		app.Application = configured
		if err := configureRendering(configured.ApplicationServer); err != nil {
			return nil, err
		}
		if selectModules != nil {
			return selectModules(ctx, configured)
		}
		return siteModules(ctx, app, configured.Config.FilePath(contactDirectory))
	}
	configured, err := framework.New(ctx, cfg, options)
	if err != nil {
		return nil, err
	}
	app.Application = configured
	mailProvider := "none"
	if options.Services.Mail != nil {
		mailProvider = "application-supplied"
	} else if configured.Mail != nil {
		mailProvider = "smtp"
	}
	logServiceProviders(logger.Get(ctx), app, mailProvider)
	return app, nil
}

// run delegates serving and resource ownership to the shared application lifecycle.
func run(ctx context.Context, s *application) error {
	return s.Run(ctx, func(ctx context.Context) (*http.Server, net.Listener, error) {
		return prepareHTTP(ctx, s)
	})
}

// The reference application's policy allows local assets and Google Fonts.
// Other applications must select their own policy for their assets and features.
const referenceContentSecurityPolicy = "default-src 'none'; " +
	"script-src 'self'; " +
	"style-src 'self' https://fonts.googleapis.com; font-src https://fonts.gstatic.com; " +
	"img-src 'self'; connect-src 'self'; base-uri 'none'; object-src 'none'; " +
	"form-action 'self'; frame-ancestors 'none'"

// prepareHTTP binds the initialized application to its loopback listener.
func prepareHTTP(ctx context.Context, s *application) (*http.Server, net.Listener, error) {
	address, err := referenceListenAddress(s.Config.HTTP.ListenHost, s.Config.HTTP.Port)
	if err != nil {
		return nil, nil, err
	}

	var checks []health.Check
	if s.contactHealth != nil {
		checks = append(checks, health.Check{Name: "contacts", Run: s.contactHealth})
	}
	if s.MailHealth != nil && s.Config.Auth.Enabled && s.Config.Auth.RegistrationEnabled && s.Config.Auth.RegisterRequiresVerification {
		checks = append(checks, health.Check{Name: "mail", Run: s.MailHealth})
	}
	handler, err := s.HealthHandler(ctx, 2*time.Second, checks...)
	if err != nil {
		return nil, nil, err
	}

	httpServer, err := server.NewHTTPServer(s.Config.HTTP, handler)
	if err != nil {
		return nil, nil, err
	}
	httpServer.Addr = address

	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, nil, err
	}
	fmt.Printf("Starting server at %s\n", listener.Addr())
	return httpServer, listener, nil
}

// referenceListenAddress keeps the development application on loopback interfaces.
// This restriction does not apply to applications built with the framework.
func referenceListenAddress(listenHost string, port uint16) (string, error) {
	host := strings.TrimSpace(listenHost)
	if host == "" {
		return "", fmt.Errorf("http.listenHost must be configured")
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return "", fmt.Errorf("reference application http.listenHost must be a loopback IP address, such as 127.0.0.1 or ::1, got %q", listenHost)
	}

	return net.JoinHostPort(ip.String(), strconv.Itoa(int(port))), nil
}
