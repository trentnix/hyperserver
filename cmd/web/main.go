// Command web runs HyperServer's loopback-only development application.
// It imports the sample site and email authentication provider. It is not suitable
// for public deployment.
package main

import (
	"context"
	"errors"
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

	"github.com/trentnix/hyperserver/pkg/ratelimit"
	"github.com/trentnix/hyperserver/pkg/requestinfo"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/middleware"
	"github.com/trentnix/hyperserver/pkg/util"
)

func main() {
	contactDirectory := flag.String("contact-directory", "", "store contact submissions as JSON files in this directory instead of SQLite")
	flag.Parse()
	configureContactStorage(*contactDirectory)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		stop()
		fmt.Println("Shutting down. Send another interrupt to force exit.")
	}()
	if err := run(ctx, server.NewApplicationServer()); err != nil {
		log.Fatal(err)
	}
}

// run owns the application pool and closes it after requests have drained,
// including when initialization or listening fails.
func run(ctx context.Context, s *server.ApplicationServer) (err error) {
	defer func() { err = errors.Join(err, s.Shutdown()) }()

	address, err := referenceListenAddress(s.Config.HTTP.ListenHost, s.Config.HTTP.Port)
	if err != nil {
		return err
	}

	// if the working directory is configured, set the working directory
	if s.Config.App.WorkingDirectory != "" {
		err := util.SetWorkingDirectory(s.Config.App.WorkingDirectory)
		if err != nil {
			return err
		}
	}

	tlsConfig, err := listenerTLS(s.Config.HTTP)
	if err != nil {
		return err
	}
	if err := setupAccountStorage(ctx, s); err != nil {
		return fmt.Errorf("failed to prepare account storage: %w", err)
	}
	l, err := logger.NewZapLogger()
	if err != nil {
		return fmt.Errorf("failed to instantiate a logger: %w", err)
	}

	// attach routes and their handlers to the router
	if err := SetupHandlers(ctx, s, l); err != nil {
		return fmt.Errorf("failed to set up the registered handlers: %w", err)
	}

	// set up authentication services and components
	if err := SetupAuthentication(s, l); err != nil {
		return fmt.Errorf("failed to set up the authorization services: %w", err)
	}

	handler, err := applicationHandler(s, l)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:         address,
		Handler:      handler,
		ReadTimeout:  s.Config.HTTP.ReadTimeout,
		WriteTimeout: s.Config.HTTP.WriteTimeout,
		IdleTimeout:  s.Config.HTTP.IdleTimeout,
		TLSConfig:    tlsConfig,
	}

	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	fmt.Printf("Starting server at %s\n", listener.Addr())
	return serveHTTP(ctx, server, listener)
}

// applicationHandler logs requests, verifies proxy metadata, applies the optional
// shared budget, checks browser origins, then loads sessions and dispatches routes.
// Inherited per-route policies and body limits are applied during registration.
// Non-browser requests without origin headers follow Go's CrossOriginProtection
// defaults. No trusted origins or CSRF exemptions are set.
func applicationHandler(s *server.ApplicationServer, l logger.Logger) (http.Handler, error) {
	proxy, err := requestinfo.NewProxyMiddleware(s.Config.HTTP.TrustedProxies)
	if err != nil {
		return nil, fmt.Errorf("http.trustedProxies: %w", err)
	}
	handler := middleware.ChainMiddleware(s.Web,
		middleware.LoadSessionManagement(s.Database, s.SessionManager),
		http.NewCrossOriginProtection().Handler,
	)
	if limit := s.Config.HTTP.SharedRateLimit; limit.Enabled {
		limiter, err := ratelimit.New("shared application", ratelimit.Policy{Requests: limit.Requests, Window: limit.Window, MaxClients: limit.MaxClients})
		if err != nil {
			return nil, fmt.Errorf("http.sharedRateLimit: %w", err)
		}
		handler = limiter.Handler(handler)
	}
	return middleware.LoggerMiddleware(l)(proxy(handler)), nil
}

// serveHTTP stops accepting requests on cancellation and waits for handlers to
// finish before returning. The caller can then close their shared dependencies.
func serveHTTP(ctx context.Context, s *http.Server, listener net.Listener) error {
	defer listener.Close()
	done := make(chan error, 1)
	go func() {
		if s.TLSConfig != nil {
			done <- s.ServeTLS(listener, "", "")
		} else {
			done <- s.Serve(listener)
		}
	}()

	var err error
	select {
	case err = <-done:
	case <-ctx.Done():
	}
	// Do not reuse the canceled context: active requests still need their pool.
	shutdownErr := s.Shutdown(context.Background())
	if err == nil {
		err = <-done
	}
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	return errors.Join(err, shutdownErr)
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
