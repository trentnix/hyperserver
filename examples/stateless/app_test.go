package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
)

func TestStatelessApplication(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)
	t.Setenv("HYPERSERVER_HTTP_PORT", "invalid")
	app, err := newApplication(context.Background(), "Independent greeting")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Shutdown(); err != nil {
			t.Error(err)
		}
	})
	for _, htmx := range []bool{false, true} {
		for _, name := range []string{"", "Ada", "<script>alert(1)</script>"} {
			r := httptest.NewRequest(http.MethodGet, "/?name="+url.QueryEscape(name), nil)
			if htmx {
				r.Header.Set("HX-Request", "true")
			}
			w := httptest.NewRecorder()
			app.Web.ServeHTTP(w, r)
			body := w.Body.String()
			if w.Code != http.StatusOK || strings.Contains(body, "<!doctype html>") == htmx {
				t.Fatalf("htmx=%t: status=%d body=%s", htmx, w.Code, body)
			}
			want := name
			if name == "" {
				want = "world"
			} else if strings.HasPrefix(name, "<script>") {
				want = "&lt;script&gt;alert(1)&lt;/script&gt;"
			}
			if !strings.Contains(body, "Hello, "+want+"!") || strings.Contains(body, "<script>") {
				t.Fatalf("incorrect or unescaped greeting: %s", body)
			}
			if !htmx && !strings.Contains(body, "Independent greeting") {
				t.Fatal("application name missing")
			}
			if len(w.Result().Cookies()) != 0 || w.Header().Get("Vary") != "HX-Request" {
				t.Fatal("response set a cookie or lost representation variation")
			}
		}
	}
	if app.Database != nil || app.SessionManager != nil || app.AccountRepository != nil || app.Mail != nil {
		t.Fatal("stateless application activated an optional service")
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatalf("application created files: entries=%v error=%v", entries, err)
	}
	workingDirectory, err := os.Getwd()
	if err != nil || workingDirectory != directory {
		t.Fatalf("application changed working directory: %q, %v", workingDirectory, err)
	}
}

func TestApplicationStartupCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app, err := newApplication(ctx, "Hello")
	if app != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled startup: app=%v error=%v", app, err)
	}
}

func TestApplicationRenderFailure(t *testing.T) {
	app, err := newApplication(context.Background(), "Hello")
	if err != nil {
		t.Fatal(err)
	}
	defer app.Shutdown()
	app.ContentManager.RegisterLayouts(contentservice.PageType, fstest.MapFS{
		"broken.html": {Data: []byte("private-template-marker {{if}}")},
	}, "broken.html")
	w := httptest.NewRecorder()
	app.Web.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusInternalServerError || w.Body.String() != "Unable to display greeting\n" {
		t.Fatalf("render failure leaked output: status=%d body=%q", w.Code, w.Body.String())
	}
}
