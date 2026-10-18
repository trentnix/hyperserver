package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestServeDrainsBeforeApplicationShutdown(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(map[bool]string{true: "timeout", false: "drained"}[timeout], func(t *testing.T) {
			app, err := newApplication(context.Background(), "Hello")
			if err != nil {
				t.Fatal(err)
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var unblock sync.Once
			var closed atomic.Int32
			app.OnShutdown("test", func(context.Context) error {
				select {
				case <-finished:
				default:
					t.Error("application closed before the request finished")
				}
				closed.Add(1)
				return nil
			})
			app.Web.HandleFunc("GET /slow", func(w http.ResponseWriter, r *http.Request) {
				defer close(finished)
				close(started)
				<-release
				_, _ = io.WriteString(w, "done")
			})
			ctx, cancel := context.WithCancel(context.Background())
			budget := time.Second
			if timeout {
				budget = 20 * time.Millisecond
			}
			app.Config.HTTP.ShutdownTimeout = &budget
			var serveErr error
			stopped := make(chan struct{})
			go func() {
				defer close(stopped)
				serveErr = app.Run(ctx, func(context.Context) (*http.Server, net.Listener, error) {
					httpServer, err := newHTTPServer(app)
					return httpServer, listener, err
				})
			}()
			t.Cleanup(func() {
				cancel()
				unblock.Do(func() { close(release) })
				_ = listener.Close()
				select {
				case <-stopped:
				case <-time.After(5 * time.Second):
					t.Error("server did not stop")
				}
			})

			requestDone := make(chan error, 1)
			go func() {
				client := &http.Client{Timeout: 5 * time.Second}
				response, err := client.Get("http://" + listener.Addr().String() + "/slow")
				if err == nil {
					_, err = io.Copy(io.Discard, response.Body)
					response.Body.Close()
					if response.Header.Get("X-Content-Type-Options") != "nosniff" {
						t.Error("missing browser security headers")
					}
				}
				requestDone <- err
			}()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("request did not start")
			}
			cancel()
			if !timeout {
				unblock.Do(func() { close(release) })
			}
			select {
			case <-stopped:
			case <-time.After(5 * time.Second):
				t.Fatal("server did not honor its shutdown budget")
			}
			if timeout {
				if !errors.Is(serveErr, context.DeadlineExceeded) || closed.Load() != 0 {
					t.Errorf("timeout: error=%v cleanup calls=%d", serveErr, closed.Load())
				}
				unblock.Do(func() { close(release) })
			} else if serveErr != nil || closed.Load() != 1 {
				t.Errorf("drained: error=%v cleanup calls=%d", serveErr, closed.Load())
			}
			select {
			case <-finished:
			case <-time.After(5 * time.Second):
				t.Fatal("handler did not finish")
			}
			if err := <-requestDone; !timeout && err != nil {
				t.Fatal(err)
			}
			if err := app.Shutdown(); err != nil {
				t.Fatal(err)
			}
			if closed.Load() != 1 {
				t.Fatal("application cleanup did not run exactly once")
			}
		})
	}
}

func TestServeFailureClosesApplication(t *testing.T) {
	app, err := newApplication(context.Background(), "Hello")
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	app.OnShutdown("test", func(context.Context) error { closed = true; return nil })
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener.Close()
	err = app.Run(context.Background(), func(context.Context) (*http.Server, net.Listener, error) {
		httpServer, err := newHTTPServer(app)
		return httpServer, listener, err
	})
	if err == nil || !closed {
		t.Fatalf("failed serving left resources open: error=%v closed=%t", err, closed)
	}
}
