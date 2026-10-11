package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// ErrHTTPDrainIncomplete means handlers may still be using application services.
// The caller must exit or wait for those handlers before closing their services.
var ErrHTTPDrainIncomplete = errors.New("HTTP shutdown did not finish draining requests; shared services left open until process exit")

// Run prepares and serves one HTTP server, then closes application resources.
// prepare configures services, modules, middleware, and the listener but must not
// start serving. Run owns the returned listener, even when prepare returns an error.
// Register other acquired resources with OnShutdown before returning from prepare.
//
// Cancel ctx to stop serving. http.shutdownTimeout controls request draining and
// defaults to ten seconds. Resource cleanup uses Shutdown's separate deadline.
// Startup failures also close application resources. If requests do not drain,
// Run closes connections and returns ErrHTTPDrainIncomplete without closing
// services that handlers may still need. The command must then exit or arrange to
// wait for handlers before cleanup. Hijacked connections need separate management.
// Call once per application. Signal handling remains the command's responsibility.
func (s *ApplicationServer) Run(ctx context.Context, prepare func(context.Context) (*http.Server, net.Listener, error)) (err error) {
	if s == nil {
		return fmt.Errorf("run requires an application")
	}
	drained := true
	defer func() {
		if drained {
			err = errors.Join(err, s.Shutdown())
		}
	}()

	timeout := 10 * time.Second
	if s.Config != nil && s.Config.HTTP.ShutdownTimeout != nil {
		timeout = *s.Config.HTTP.ShutdownTimeout
	}
	if timeout <= 0 {
		return fmt.Errorf("http.shutdownTimeout must be positive")
	}
	if prepare == nil {
		return fmt.Errorf("run requires a preparation function")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	httpServer, listener, err := prepare(ctx)
	if listener != nil {
		defer listener.Close()
	}
	if err != nil {
		return err
	}
	if httpServer == nil || listener == nil {
		return fmt.Errorf("preparation must return an HTTP server and listener")
	}

	err = ServeHTTP(ctx, httpServer, listener, timeout)
	drained = !errors.Is(err, ErrHTTPDrainIncomplete)
	return err
}

// ServeHTTP serves an existing listener until ctx is canceled or serving fails.
// It uses ServeTLS when s.TLSConfig is non-nil, with certificates supplied there.
// It closes the listener and drains requests using a fresh, positive timeout.
// On drain failure it closes connections and returns ErrHTTPDrainIncomplete.
// It does not close application services. Use ApplicationServer.Run to manage them.
// The server must not already be serving. Hijacked connections are caller-owned.
func ServeHTTP(ctx context.Context, s *http.Server, listener net.Listener, timeout time.Duration) error {
	if listener != nil {
		defer listener.Close()
	}
	if s == nil || listener == nil {
		return fmt.Errorf("serving requires an HTTP server and listener")
	}
	if timeout <= 0 {
		return fmt.Errorf("HTTP drain timeout must be positive")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

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

	// Active requests need a drain budget independent of the canceled run context.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	shutdownErr := s.Shutdown(shutdownCtx)
	if shutdownErr != nil {
		shutdownErr = errors.Join(ErrHTTPDrainIncomplete, shutdownErr, s.Close())
	}
	if err == nil {
		err = <-done
	}
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	return errors.Join(err, shutdownErr)
}
