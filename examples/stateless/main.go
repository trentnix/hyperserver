// Command stateless runs a loopback-only, stateless HyperServer example.
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
	app.Config.HTTP.ShutdownTimeout = timeout
	if err := app.Run(ctx, func(context.Context) (*http.Server, net.Listener, error) {
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
		if err != nil {
			return nil, nil, err
		}
		log.Printf("Hello application listening at http://%s", listener.Addr())
		httpServer, err := newHTTPServer(app)
		return httpServer, listener, err
	}); err != nil {
		log.Fatal(err)
	}
}

func newHTTPServer(app *server.ApplicationServer) (*http.Server, error) {
	handler, err := middleware.Web(app.Web, app.Config.HTTP,
		"default-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'", nil)
	if err != nil {
		return nil, err
	}
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       time.Minute,
	}, nil
}
