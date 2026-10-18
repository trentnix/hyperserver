package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"

	authmiddleware "github.com/trentnix/hyperserver/auth/middleware"
	"github.com/trentnix/hyperserver/cmd/internal/setup"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/middleware"
	"github.com/trentnix/hyperserver/pkg/services/session"
)

// The reference application's policy allows local assets and Google Fonts.
// Other applications must select their own policy for their assets and features.
const referenceContentSecurityPolicy = "default-src 'none'; " +
	"script-src 'self'; " +
	"style-src 'self' https://fonts.googleapis.com; font-src https://fonts.gstatic.com; " +
	"img-src 'self'; connect-src 'self'; base-uri 'none'; object-src 'none'; " +
	"form-action 'self'; frame-ancestors 'none'"

// prepareHTTP selects the reference application's services, modules, and HTTP stack.
func prepareHTTP(ctx context.Context, s *application, accounts *setup.AccountStorage, contactDirectory string) (*http.Server, net.Listener, error) {
	address, err := referenceListenAddress(s.Config.HTTP.ListenHost, s.Config.HTTP.Port)
	if err != nil {
		return nil, nil, err
	}

	// Resolve relative resource paths before any service opens files or connections.
	if err := s.Config.ResolvePaths(); err != nil {
		return nil, nil, err
	}

	// Open shared SQL storage only for selected consumers.
	if contactDirectory == "" || (s.AccountRepository == nil && accounts.NeedsSQLPool()) {
		if err := s.initializeDatabase(ctx); err != nil {
			return nil, nil, fmt.Errorf("failed to prepare application database: %w", err)
		}
	}
	mailProvider := "application-supplied"
	if s.Mail == nil {
		mailProvider = "smtp"
	}
	if err := initializeMail(s); err != nil {
		return nil, nil, fmt.Errorf("failed to prepare mail: %w", err)
	}

	if err := initializeSessions(ctx, s); err != nil {
		return nil, nil, fmt.Errorf("failed to prepare session storage: %w", err)
	}
	if err := initializeAccounts(ctx, s, accounts); err != nil {
		return nil, nil, fmt.Errorf("failed to prepare account storage: %w", err)
	}
	l := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := configureRendering(s.ApplicationServer); err != nil {
		return nil, nil, err
	}
	catalog, err := siteModules(s, s.Config.FilePath(contactDirectory))
	if err != nil {
		return nil, nil, err
	}

	// attach routes and their handlers to the router
	if err := SetupHandlers(ctx, s.ApplicationServer, l, catalog, nil); err != nil {
		return nil, nil, fmt.Errorf("failed to set up the registered handlers: %w", err)
	}
	logServiceProviders(l, s, mailProvider)

	handler, err := applicationHandler(s, l)
	if err != nil {
		return nil, nil, err
	}
	handler, err = healthHandler(logger.Set(ctx, l), s, handler)
	if err != nil {
		return nil, nil, err
	}
	httpServer, err := server.NewHTTPServer(s.Config.HTTP, handler)
	if err != nil {
		return nil, nil, err
	}
	// Keep the reference application's loopback policy outside the library.
	httpServer.Addr = address

	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, nil, err
	}
	fmt.Printf("Starting server at %s\n", listener.Addr())
	return httpServer, listener, nil
}

// applicationHandler logs requests and sets browser security headers, then verifies
// proxy metadata before applying HSTS, the optional shared budget, origin checks,
// session loading, and route dispatch.
// Inherited per-route policies and body limits are applied during registration.
// Non-browser requests without origin headers follow Go's CrossOriginProtection
// defaults. No trusted origins or CSRF exemptions are set.
func applicationHandler(s *application, l *slog.Logger) (http.Handler, error) {
	loadSession := session.WithSessionManager(s.SessionManager)
	if s.Config.Auth.Enabled {
		loadSession = authmiddleware.LoadSessionManagement(s.AccountRepository, s.SessionManager)
	}
	return middleware.Web(loadSession(s.Web), s.Config.HTTP, referenceContentSecurityPolicy, l)
}

// referenceListenAddress keeps the development application on loopback interfaces.
// This restriction does not apply to applications built with the framework.
func referenceListenAddress(listenHost string, port uint16) (string, error) {
	host := strings.TrimSpace(listenHost)
	if host == "" || strings.EqualFold(host, "localhost") {
		host = "127.0.0.1"
	}

	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return "", fmt.Errorf("reference application must listen on a loopback address, got %q", listenHost)
	}

	return net.JoinHostPort(ip.String(), strconv.Itoa(int(port))), nil
}
