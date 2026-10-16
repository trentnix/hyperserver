// Command greeting demonstrates consuming HyperServer from another Go module.
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

	"example.com/greeting/greeting"
	"example.com/greeting/notes"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/middleware"
)

func main() {
	port := flag.Int("port", 8083, "loopback HTTP port (0 chooses an available port)")
	provider := flag.String("notes-store", "files", "notes storage provider: files or sqlite")
	path := flag.String("notes-path", "", "notes storage location (defaults depend on provider)")
	flag.Parse()
	if *port < 0 || *port > 65535 {
		log.Fatal("port must be between 0 and 65535")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	app, err := newApplication(ctx, *provider, *path)
	if err != nil {
		log.Fatal(err)
	}
	if err := app.Run(ctx, func(context.Context) (*http.Server, net.Listener, error) {
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
		if err != nil {
			return nil, nil, err
		}
		log.Printf("Example application listening at http://%s", listener.Addr())
		return newHTTPServer(app), listener, nil
	}); err != nil {
		log.Fatal(err)
	}
}

func newApplication(ctx context.Context, provider, path string) (*server.ApplicationServer, error) {
	app, err := server.NewApplicationServer(config.Config{App: config.AppConfig{Name: "External example"}})
	if err != nil {
		return nil, err
	}
	repository, closeStore, err := openNotes(ctx, provider, path)
	if err != nil {
		return nil, errors.Join(err, app.Shutdown())
	}
	if closeStore != nil {
		app.OnShutdown("notes storage", func(context.Context) error { return closeStore() })
	}
	if err := greeting.ConfigureRendering(app.ContentManager); err != nil {
		return nil, errors.Join(err, app.Shutdown())
	}
	catalog, err := handlers.Replace(handlers.Registered(), notes.Module(repository))
	if err != nil {
		return nil, errors.Join(err, app.Shutdown())
	}
	if err := handlers.Setup(ctx, app, nil, catalog, nil); err != nil {
		return nil, errors.Join(err, app.Shutdown())
	}
	return app, nil
}

func newHTTPServer(app *server.ApplicationServer) *http.Server {
	return &http.Server{
		Handler:           middleware.SecurityHeaders("default-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")(http.NewCrossOriginProtection().Handler(app.Web)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       time.Minute,
	}
}
