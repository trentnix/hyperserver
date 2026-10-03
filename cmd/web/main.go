// Command web runs HyperServer's loopback-only development application.
// It imports the sample site and email authentication provider. It is not suitable
// for public deployment.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/middleware"
	"github.com/trentnix/hyperserver/pkg/util"
)

func main() {
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

	if err := setupAccountStorage(ctx, s); err != nil {
		return fmt.Errorf("failed to prepare account storage: %w", err)
	}

	// attach routes and their handlers to the router
	if err := SetupHandlers(ctx, s); err != nil {
		return fmt.Errorf("failed to set up the registered handlers: %w", err)
	}

	// set up authentication services and components
	if err := SetupAuthentication(s); err != nil {
		return fmt.Errorf("failed to set up the authorization services: %w", err)
	}

	l, err := logger.NewZapLogger()
	if err != nil {
		return fmt.Errorf("failed to instantiate a logger: %w", err)
	}

	mux := middleware.ChainMiddleware(s.Web,
		middleware.LoggerMiddleware(l),
		middleware.LoadSessionManagement(s.Database, s.SessionManager),
	)

	server := &http.Server{
		Addr:         address,
		Handler:      mux,
		ReadTimeout:  s.Config.HTTP.ReadTimeout,
		WriteTimeout: s.Config.HTTP.WriteTimeout,
		IdleTimeout:  s.Config.HTTP.IdleTimeout,
	}

	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	fmt.Printf("Starting server at %s\n", listener.Addr())
	return serveHTTP(ctx, server, listener)
}

// serveHTTP stops accepting requests on cancellation and waits for handlers to
// finish before returning. The caller can then close their shared dependencies.
func serveHTTP(ctx context.Context, s *http.Server, listener net.Listener) error {
	done := make(chan error, 1)
	go func() { done <- s.Serve(listener) }()

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
