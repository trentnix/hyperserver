package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/pkg/handlers"
)

func TestExternalApplication(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)
	t.Setenv("HYPERSERVER_HTTP_PORT", "invalid")
	catalog := handlers.Registered()
	if len(catalog) != 3 {
		t.Fatalf("unexpected imported modules: %+v", catalog)
	}
	app, err := newApplication(t.Context(), testSettings(t, "sqlite", filepath.Join(t.TempDir(), "notes.db")))
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		app.Shutdown()
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- app.Run(ctx, func(context.Context) (*http.Server, net.Listener, error) {
			return newHTTPServer(app), listener, nil
		})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(15 * time.Second):
			t.Error("application did not shut down")
		}
	})
	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}

	for _, variant := range []struct {
		name        string
		hx, history bool
	}{
		{name: "page"},
		{name: "fragment", hx: true},
		{name: "history", hx: true, history: true},
	} {
		for _, tc := range []struct {
			name, path, want string
			status           int
		}{
			{"initial", "/", "Your name", 200},
			{"valid", "/?name=Ada", "Hello, Ada!", 200},
			{"invalid", "/?name=+", "Enter your name.", 200},
			{"escaping", "/?name=" + url.QueryEscape("<script>alert(1)</script>"), "Hello, &lt;script&gt;alert(1)&lt;/script&gt;!", 200},
			{"malformed", "/?name=%zz", "Unable to parse form data", 400},
			{"oversized", "/?name=" + strings.Repeat("a", 17*1024), "Form field is too large", 413},
			{"missing", "/missing", "404 page not found", 404},
		} {
			t.Run(variant.name+"/"+tc.name, func(t *testing.T) {
				r, err := http.NewRequestWithContext(t.Context(), "GET", "http://"+listener.Addr().String()+tc.path, nil)
				if err != nil {
					t.Fatal(err)
				}
				if variant.hx {
					r.Header.Set("HX-Request", "true")
				}
				if variant.history {
					r.Header.Set("HX-History-Restore-Request", "true")
				}
				response, err := client.Do(r)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				body, err := io.ReadAll(response.Body)
				if err != nil {
					t.Fatal(err)
				}
				text := string(body)
				if response.StatusCode != tc.status || !strings.Contains(text, tc.want) || strings.Contains(text, "<script>") {
					t.Fatalf("status=%d body=%s", response.StatusCode, text)
				}
				if tc.status == 200 {
					if full := strings.Contains(text, "<!doctype html>"); full != (!variant.hx || variant.history) {
						t.Fatal("wrong response representation")
					}
					if styled := strings.Contains(text, `href="/assets/app.css"`); styled != (!variant.hx || variant.history) {
						t.Fatal("stylesheet must appear only in full-page responses")
					}
					policy := response.Header.Get("Content-Security-Policy")
					if !strings.Contains(policy, "style-src 'self'") || strings.Contains(policy, "unsafe-inline") {
						t.Fatalf("unexpected stylesheet policy: %s", policy)
					}
					if response.Header.Get("Cache-Control") != "no-store" || !strings.Contains(response.Header.Get("Vary"), "HX-Request") {
						t.Fatal("missing cache policy")
					}
				}
				if len(response.Cookies()) != 0 {
					t.Fatal("anonymous greeting response issued cookies")
				}
				if tc.name == "invalid" && strings.Contains(text, "Hello,") {
					t.Fatal("invalid input produced a greeting")
				}
			})
		}
	}
	for _, method := range []string{"GET", "HEAD", "POST"} {
		r, err := http.NewRequestWithContext(t.Context(), method, "http://"+listener.Addr().String()+"/assets/app.css", nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if method == "POST" {
			if response.StatusCode != http.StatusMethodNotAllowed {
				t.Fatalf("stylesheet POST status = %d", response.StatusCode)
			}
			continue
		}
		if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/css") {
			t.Fatalf("stylesheet %s: status=%d type=%s", method, response.StatusCode, response.Header.Get("Content-Type"))
		}
		if method == "GET" && !strings.Contains(string(body), ".note-card") || method == "HEAD" && len(body) != 0 {
			t.Fatalf("unexpected stylesheet %s body", method)
		}
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatalf("unexpected runtime files: %v, %v", entries, err)
	}
}

func TestCanceledStartup(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	app, err := newApplication(ctx, testSettings(t, "sqlite", filepath.Join(t.TempDir(), "notes.db")))
	if app != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("app=%v err=%v", app, err)
	}
}
