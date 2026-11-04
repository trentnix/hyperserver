package application_test

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/internal/test/tlstest"
	"github.com/trentnix/hyperserver/pkg/server"
)

// Run must drain requests before closing consumers and their shared resources.
// A drain timeout leaves both open until the caller can safely clean them up.
func TestRunDrainResourceOwnership(t *testing.T) {
	for _, secure := range []bool{false, true} {
		for _, timeout := range []bool{false, true} {
			t.Run(fmt.Sprintf("tls=%t/timeout=%t", secure, timeout), func(t *testing.T) {
				db := shutdownTestDB(t)
				if _, err := db.ExecContext(t.Context(), "CREATE TABLE writes (value TEXT)"); err != nil {
					t.Fatal(err)
				}
				budget := 3 * time.Second
				if timeout {
					budget = 30 * time.Millisecond
				}
				app, err := server.NewApplicationServer(config.Config{HTTP: config.HTTPConfig{ShutdownTimeout: &budget}})
				if err != nil {
					t.Fatal(err)
				}
				app.OnShutdown("pool", func(context.Context) error { return db.Close() })
				var consumerClosed atomic.Bool
				app.OnShutdown("consumer", func(ctx context.Context) error {
					consumerClosed.Store(true)
					return db.PingContext(ctx)
				})
				started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
				var once sync.Once
				releaseRequest := func() { once.Do(func() { close(release) }) }
				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					defer close(finished)
					close(started)
					<-release
					// Ignore request cancellation to model a handler still using shared services.
					if _, err := db.ExecContext(context.Background(), "INSERT INTO writes VALUES ('drained')"); err != nil {
						t.Error(err)
					}
					w.WriteHeader(http.StatusNoContent)
				})
				cfg := config.HTTPConfig{}
				transport := &http.Transport{}
				scheme := "http"
				if secure {
					tlsConfig, roots := tlstest.Files(t)
					cfg.TLS = tlsConfig.TLS
					transport.TLSClientConfig = &tls.Config{RootCAs: roots}
					scheme = "https"
				}
				defer transport.CloseIdleConnections()
				httpServer, err := server.NewHTTPServer(cfg, handler)
				if err != nil {
					t.Fatal(err)
				}
				listener, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				closing := &observedListener{Listener: listener, closed: make(chan struct{})}
				ctx, cancel := context.WithCancel(t.Context())
				done := make(chan error, 1)
				exited := make(chan struct{})
				go func() {
					defer close(exited)
					done <- app.Run(ctx, func(context.Context) (*http.Server, net.Listener, error) { return httpServer, closing, nil })
				}()
				defer func() {
					cancel()
					releaseRequest()
					awaitSignal(t, exited)
					httpServer.Close()
					app.Shutdown()
				}()
				client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
				response := make(chan error, 1)
				go func() {
					res, err := client.Get(scheme + "://" + listener.Addr().String())
					if res != nil {
						io.Copy(io.Discard, res.Body)
						res.Body.Close()
					}
					response <- err
				}()
				awaitSignal(t, started)
				cancel()
				awaitSignal(t, closing.closed)
				if timeout {
					select {
					case err := <-done:
						if !errors.Is(err, server.ErrHTTPDrainIncomplete) || !errors.Is(err, context.DeadlineExceeded) {
							t.Fatalf("drain failure: %v", err)
						}
					case <-time.After(5 * time.Second):
						t.Fatal("drain did not time out")
					}
				} else {
					select {
					case err := <-done:
						t.Fatalf("Run returned before draining: %v", err)
					default:
					}
				}
				if consumerClosed.Load() || db.Ping() != nil {
					t.Fatal("services closed under an active request")
				}
				releaseRequest()
				awaitSignal(t, finished)
				if timeout {
					if err := app.Shutdown(); err != nil {
						t.Fatal(err)
					}
				} else {
					awaitResult(t, response)
					awaitResult(t, done)
				}
				if !consumerClosed.Load() || db.Ping() == nil {
					t.Fatal("shutdown left owned services open")
				}
			})
		}
	}
}
