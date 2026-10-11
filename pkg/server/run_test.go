package server

import (
	"context"
	"crypto/tls"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
)

// No requests can be admitted by this listener. Serving must preserve its error
// and close it before releasing application resources.
type runTestListener struct {
	err    error
	closed bool
}

func (l *runTestListener) Accept() (net.Conn, error) { return nil, l.err }
func (l *runTestListener) Close() error              { l.closed = true; return nil }
func (*runTestListener) Addr() net.Addr              { return &net.TCPAddr{} }

func TestRunCleansUpStartupFailures(t *testing.T) {
	for _, mode := range []string{"prepare", "partial listener", "missing server", "missing listener", "accept", "TLS", "cancel during prepare"} {
		t.Run(mode, func(t *testing.T) {
			app, err := NewApplicationServer(config.Config{})
			if err != nil {
				t.Fatal(err)
			}
			cause, cleanupErr := errors.New("startup failed"), errors.New("cleanup failed")
			listener := &runTestListener{err: cause}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			closed := 0
			app.OnShutdown("owned resource", func(ctx context.Context) error {
				closed++
				if ctx.Err() != nil {
					t.Error("cleanup inherited startup cancellation")
				}
				if _, ok := ctx.Deadline(); !ok {
					t.Error("cleanup has no deadline")
				}
				if mode != "prepare" && mode != "missing listener" && !listener.closed {
					t.Error("cleanup ran before the listener closed")
				}
				return cleanupErr
			})
			err = app.Run(ctx, func(got context.Context) (*http.Server, net.Listener, error) {
				if got != ctx {
					t.Error("preparation did not receive the run context")
				}
				s := &http.Server{}
				switch mode {
				case "prepare":
					return nil, nil, cause
				case "partial listener":
					return s, listener, cause
				case "missing server":
					return nil, listener, nil
				case "missing listener":
					return s, nil, nil
				case "TLS":
					s.TLSConfig = &tls.Config{} // No certificate: serving must fail.
				case "cancel during prepare":
					cancel()
				}
				return s, listener, nil
			})
			if !errors.Is(err, cleanupErr) || closed != 1 {
				t.Fatalf("cleanup: error=%v calls=%d", err, closed)
			}
			if mode == "prepare" || mode == "partial listener" || mode == "accept" {
				if !errors.Is(err, cause) {
					t.Fatal("startup error lost:", err)
				}
			}
			if mode == "cancel during prepare" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost:", err)
			}
			if mode == "TLS" && !errors.Is(err, fs.ErrNotExist) {
				t.Fatal("TLS failure lost:", err)
			}
			if err := app.Shutdown(); err != nil || closed != 1 {
				t.Fatalf("repeated cleanup: error=%v calls=%d", err, closed)
			}
		})
	}
}

func TestRunRejectsInvalidStartupBeforePreparing(t *testing.T) {
	for _, mode := range []string{"zero timeout", "negative timeout", "canceled", "nil prepare"} {
		t.Run(mode, func(t *testing.T) {
			app := &ApplicationServer{Config: &config.Config{}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			closed := false
			app.OnShutdown("test", func(context.Context) error { closed = true; return nil })
			prepare := func(context.Context) (*http.Server, net.Listener, error) {
				t.Fatal("invalid startup called prepare")
				return nil, nil, nil
			}
			switch mode {
			case "zero timeout", "negative timeout":
				timeout := time.Duration(0)
				if mode == "negative timeout" {
					timeout = -time.Second
				}
				app.Config.HTTP.ShutdownTimeout = &timeout
			case "canceled":
				cancel()
			case "nil prepare":
				prepare = nil
			}
			if err := app.Run(ctx, prepare); err == nil || !closed {
				t.Fatalf("invalid startup: error=%v closed=%t", err, closed)
			}
		})
	}
}

func TestServeHTTPRejectsInvalidInputs(t *testing.T) {
	for _, mode := range []string{"nil server", "nil listener", "zero timeout", "negative timeout", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			listener := &runTestListener{err: errors.New("must not accept")}
			var input net.Listener = listener
			s := &http.Server{}
			timeout := time.Second
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "nil server":
				s = nil
			case "nil listener":
				input = nil
			case "zero timeout":
				timeout = 0
			case "negative timeout":
				timeout = -time.Second
			case "canceled":
				cancel()
			}
			if err := ServeHTTP(ctx, s, input, timeout); err == nil {
				t.Fatal("invalid serving inputs accepted")
			}
			if input != nil && !listener.closed {
				t.Fatal("listener left open")
			}
		})
	}
}
