// Command stateless runs a loopback-only, stateless HyperServer example.
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
	"syscall"
	"time"

	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/middleware"
)

func main() {
	port := flag.Int("port", 8082, "loopback HTTP port")
	name := flag.String("name", "Hello HyperServer", "application name")
	timeout := flag.Duration("shutdown-timeout", 10*time.Second, "time allowed for active requests to drain")
	flag.Parse()
	if *port < 1 || *port > 65535 || *timeout <= 0 {
		log.Fatal("port must be between 1 and 65535 and shutdown-timeout must be positive")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		stop() // A second signal forces exit.
	}()
	app, err := newApplication(ctx, *name)
	if err != nil {
		log.Fatal(err)
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		log.Fatal(errors.Join(err, app.Shutdown()))
	}
	log.Printf("Hello application listening at http://%s", listener.Addr())
	if err := serve(ctx, app, listener, *timeout); err != nil {
		log.Fatal(err)
	}
}

// serve drains HTTP requests before releasing application resources. If draining
// fails, the command exits without closing services underneath active handlers.
func serve(ctx context.Context, app *server.ApplicationServer, listener net.Listener, timeout time.Duration) (err error) {
	defer listener.Close()
	drained := true
	defer func() {
		if drained {
			err = errors.Join(err, app.Shutdown())
		}
	}()
	if timeout <= 0 {
		return fmt.Errorf("shutdown timeout must be positive")
	}
	handler := middleware.SecurityHeaders("default-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")(app.Web)
	httpServer := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       time.Minute,
	}
	done := make(chan error, 1)
	go func() { done <- httpServer.Serve(listener) }()
	select {
	case err = <-done:
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	shutdownErr := httpServer.Shutdown(shutdownCtx)
	if shutdownErr != nil {
		drained = false
		shutdownErr = errors.Join(fmt.Errorf("HTTP requests did not drain: %w", shutdownErr), httpServer.Close())
	}
	if err == nil {
		err = <-done
	}
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	return errors.Join(err, shutdownErr)
}
