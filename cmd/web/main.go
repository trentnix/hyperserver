// Command web runs HyperServer's loopback-only development application.
// It imports the sample site and email authentication provider. It is not suitable
// for public deployment.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/ratelimit"
	"github.com/trentnix/hyperserver/pkg/requestinfo"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/middleware"
)

func main() {
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
		log.Fatal(err)
	}
	app, err := server.NewApplicationServer(cfg)
	if err != nil {
		log.Fatal(err)
	}
	if err := run(ctx, app, *contactDirectory); err != nil {
		log.Fatal(err)
	}
}

// run delegates serving and resource ownership to the shared application lifecycle.
func run(ctx context.Context, s *server.ApplicationServer, contactDirectory string) error {
	return s.Run(ctx, func(ctx context.Context) (*http.Server, net.Listener, error) {
		return prepareHTTP(ctx, s, contactDirectory)
	})
}

// prepareHTTP selects the reference application's services, modules, and HTTP stack.
func prepareHTTP(ctx context.Context, s *server.ApplicationServer, contactDirectory string) (*http.Server, net.Listener, error) {
	address, err := referenceListenAddress(s.Config.HTTP.ListenHost, s.Config.HTTP.Port)
	if err != nil {
		return nil, nil, err
	}

	// Resolve relative resource paths before any service opens files or connections.
	if err := s.Config.ResolvePaths(); err != nil {
		return nil, nil, err
	}

	// The reference site's contact storage uses this pool. Mail is used by its
	// sample routes and the email authentication provider. Neither is a core requirement.
	if err := s.InitializeDatabase(ctx); err != nil {
		return nil, nil, fmt.Errorf("failed to prepare application database: %w", err)
	}
	if err := s.InitializeMail(); err != nil {
		return nil, nil, fmt.Errorf("failed to prepare mail: %w", err)
	}

	tlsConfig, err := listenerTLS(s.Config.HTTP)
	if err != nil {
		return nil, nil, err
	}
	if err := s.InitializeSessions(ctx); err != nil {
		return nil, nil, fmt.Errorf("failed to prepare session storage: %w", err)
	}
	if err := s.InitializeAccounts(ctx); err != nil {
		return nil, nil, fmt.Errorf("failed to prepare account storage: %w", err)
	}
	l := logger.NewSlogLogger(nil)
	catalog, err := siteModules(s.Config.FilePath(contactDirectory))
	if err != nil {
		return nil, nil, err
	}

	// attach routes and their handlers to the router
	if err := SetupHandlers(ctx, s, l, catalog, nil); err != nil {
		return nil, nil, fmt.Errorf("failed to set up the registered handlers: %w", err)
	}

	handler, err := applicationHandler(s, l)
	if err != nil {
		return nil, nil, err
	}
	httpServer := &http.Server{
		Addr:         address,
		Handler:      handler,
		ReadTimeout:  s.Config.HTTP.ReadTimeout,
		WriteTimeout: s.Config.HTTP.WriteTimeout,
		IdleTimeout:  s.Config.HTTP.IdleTimeout,
		TLSConfig:    tlsConfig,
	}

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
func applicationHandler(s *server.ApplicationServer, l logger.Logger) (http.Handler, error) {
	proxy, err := requestinfo.NewProxyMiddleware(s.Config.HTTP.TrustedProxies)
	if err != nil {
		return nil, fmt.Errorf("http.trustedProxies: %w", err)
	}
	hsts, err := middleware.NewHSTS(s.Config.HTTP.HSTSMaxAge)
	if err != nil {
		return nil, fmt.Errorf("http.hstsMaxAge: %w", err)
	}
	loadSession := middleware.WithSessionManager(s.SessionManager)
	if s.Config.Auth.Enabled {
		loadSession = middleware.LoadSessionManagement(s.AccountRepository, s.SessionManager)
	}
	handler := middleware.ChainMiddleware(s.Web,
		loadSession,
		http.NewCrossOriginProtection().Handler,
	)
	if limit := s.Config.HTTP.SharedRateLimit; limit.Enabled {
		limiter, err := ratelimit.New("shared application", ratelimit.Policy{Requests: limit.Requests, Window: limit.Window, MaxClients: limit.MaxClients})
		if err != nil {
			return nil, fmt.Errorf("http.sharedRateLimit: %w", err)
		}
		handler = limiter.Handler(handler)
	}
	handler = proxy(hsts(handler))
	handler = middleware.SecurityHeaders(referenceContentSecurityPolicy)(handler)
	return middleware.LoggerMiddleware(l)(handler), nil
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
