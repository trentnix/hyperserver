package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"example.com/greeting/greeting"
	"example.com/greeting/notes"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/server"
)

func noteRequest(t *testing.T, handler http.Handler, method, path, body string, hx, history bool) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "http://example.test"+path, strings.NewReader(body))
	if method == "POST" {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", "http://example.test")
	}
	if hx {
		r.Header.Set("HX-Request", "true")
	}
	if history {
		r.Header.Set("HX-History-Restore-Request", "true")
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestNotesWorkflow(t *testing.T) {
	for _, provider := range []string{"sqlite", "files"} {
		t.Run(provider, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "notes")
			settings := testSettings(t, provider, path)
			app, err := newApplication(t.Context(), settings)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := app.Shutdown(); err != nil {
					t.Error(err)
				}
			})
			handler := loggedInHandler(t, app)
			for _, variant := range []struct {
				name        string
				hx, history bool
			}{
				{name: "page"}, {name: "fragment", hx: true}, {name: "history", hx: true, history: true},
			} {
				t.Run(variant.name, func(t *testing.T) {
					for _, tc := range []struct {
						body   string
						status int
					}{
						{"text=", 200}, {"text=+", 200}, {"text=" + strings.Repeat("a", 1001), 200},
						{"text=%zz", 400}, {"text=" + strings.Repeat("a", 17*1024), 413}, {"text=" + strings.Repeat("a", 65*1024), 413},
					} {
						w := noteRequest(t, handler, "POST", "/notes", tc.body, variant.hx, variant.history)
						if w.Code != tc.status || w.Header().Get("Location") != "" || w.Header().Get("HX-Redirect") != "" {
							t.Fatalf("invalid status=%d body=%s", w.Code, w.Body)
						}
						if tc.status == 200 {
							if !strings.Contains(w.Body.String(), notes.ErrInvalidText.Error()) || strings.Contains(w.Body.String(), "<!doctype html>") != (!variant.hx || variant.history) {
								t.Fatal("validation response lost its errors or layout", w.Body)
							}
						}
					}
					text := variant.name + " <script>alert(1)</script> & café"
					w := noteRequest(t, handler, "POST", "/notes", url.Values{"text": {text}}.Encode(), variant.hx, variant.history)
					if variant.hx && !variant.history {
						if w.Code != 200 || w.Header().Get("HX-Redirect") != "/notes" {
							t.Fatalf("HTMX redirect = %d %v", w.Code, w.Header())
						}
					} else if w.Code != 303 || w.Header().Get("Location") != "/notes" {
						t.Fatalf("redirect = %d %v", w.Code, w.Header())
					}
					w = noteRequest(t, handler, "GET", "/notes", "", variant.hx, variant.history)
					body := w.Body.String()
					if w.Code != 200 || !strings.Contains(body, variant.name+" &lt;script&gt;") || strings.Contains(body, "<script>") || strings.Contains(body, "<!doctype html>") != (!variant.hx || variant.history) {
						t.Fatalf("list = %d %s", w.Code, body)
					}
					if w.Header().Get("Cache-Control") != "no-store" {
						t.Fatal("unexpected cache or session behavior")
					}
				})
			}
			r := httptest.NewRequest("POST", "http://example.test/notes", strings.NewReader("text=forbidden"))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set("Origin", "https://attacker.test")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != 403 {
				t.Fatalf("cross-origin write = %d", w.Code)
			}
			w = noteRequest(t, handler, "POST", "/notes?text=must-not-bind", "", false, false)
			if w.Code != 200 || !strings.Contains(w.Body.String(), notes.ErrInvalidText.Error()) {
				t.Fatal("bound note from query instead of POST form")
			}
			if w := noteRequest(t, handler, "DELETE", "/notes", "", false, false); w.Code != 405 {
				t.Fatal("unexpected method accepted")
			}

			if err := app.Shutdown(); err != nil {
				t.Fatal(err)
			}
			reopened, err := newApplication(t.Context(), settings)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Shutdown()
			// Use a real listener for the persisted read as well as handler tests.
			srv := httptest.NewServer(newHTTPServer(reopened).Handler)
			defer srv.Close()
			response, err := srv.Client().Get(srv.URL + "/notes")
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil || response.StatusCode != 200 || strings.Count(string(body), "<li>") != 3 {
				t.Fatalf("persisted list = %s, %v", body, err)
			}
		})
	}
}

type failedNotes struct{ writes atomic.Int32 }

func (f *failedNotes) Create(context.Context, notes.Note) error {
	f.writes.Add(1)
	return errors.New("private-storage-detail")
}
func (*failedNotes) List(context.Context, int) ([]notes.Note, error) {
	return nil, errors.New("private-storage-detail")
}

func TestNotesStorageFailures(t *testing.T) {
	repository := &failedNotes{}
	app, err := server.NewApplicationServer(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Shutdown()
	if err := greeting.ConfigureRendering(app.ContentManager); err != nil {
		t.Fatal(err)
	}
	app.ContentManager.BuildLayoutData = func(*http.Request) any { return true }
	allow := func(next http.Handler) http.Handler { return next }
	if err := handlers.Setup(t.Context(), app, nil, []handlers.Descriptor{notes.Module(repository, allow)}, nil); err != nil {
		t.Fatal(err)
	}
	handler := app.Web
	for _, hx := range []bool{false, true} {
		before := repository.writes.Load()
		w := noteRequest(t, handler, "POST", "/notes", "text=+", hx, false)
		if w.Code != 200 || repository.writes.Load() != before {
			t.Fatal("invalid input reached storage")
		}
		for _, method := range []string{"GET", "POST"} {
			w := noteRequest(t, handler, method, "/notes", "text=valid", hx, false)
			if w.Code != 503 || strings.Contains(w.Body.String(), "private-storage-detail") || w.Header().Get("HX-Redirect") != "" || w.Header().Get("Location") != "" {
				t.Fatalf("storage failure = %d %s", w.Code, w.Body)
			}
		}
	}
}

func TestNotesStartupFailure(t *testing.T) {
	for _, provider := range []string{"unknown", "sqlite"} {
		app, err := newApplication(t.Context(), testSettings(t, provider, filepath.Join(t.TempDir(), "missing", "notes")))
		if app != nil || err == nil {
			t.Fatalf("unexpected startup success for %s", provider)
		}
	}
}

func TestNotesRequiresWritePolicy(t *testing.T) {
	app, err := server.NewApplicationServer(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Shutdown()
	err = handlers.Setup(t.Context(), app, nil, []handlers.Descriptor{notes.Module(&failedNotes{}, nil)}, nil)
	if err == nil || !strings.Contains(err.Error(), "write-access policy") {
		t.Fatalf("missing access policy = %v", err)
	}
}
