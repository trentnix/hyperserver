package content

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/trentnix/hyperserver/pkg/components/messages"
	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/logger"
)

const responsePartials = `{{define "message"}}message:{{.Data}}{{end}}{{define "error"}}error:{{.Data}}{{end}}{{define "missing"}}missing:{{.Data}}{{end}}`

func responseManager() (*contentservice.ContentManagerService, *countingTemplateFS) {
	files := &countingTemplateFS{FS: fstest.MapFS{
		"page.html":      {Data: []byte(`<main>{{renderPartial .PartialName .}}</main>`)},
		"fragment.html":  {Data: []byte(`{{renderPartial .PartialName .}}`)},
		"responses.html": {Data: []byte(responsePartials)},
	}}
	cm := contentservice.NewContentManager()
	cm.AddPageLayout(files, "page.html")
	cm.AddHtmxLayout(files, "fragment.html")
	return cm, files
}

func responseSelection() ResponseTemplates {
	return ResponseTemplates{Message: "message", Error: "error", NotFound: "missing"}
}

func TestConfiguredResponses(t *testing.T) {
	cm, files := responseManager()
	if err := ConfigureResponses(cm, responseSelection(), TemplatePath{FS: files, Path: "responses.html"}); err != nil {
		t.Fatal(err)
	}
	if files.reads.Load() != 0 {
		t.Fatal("configuration read templates before startup")
	}
	if err := cm.PrepareTemplates(); err != nil {
		t.Fatal(err)
	}
	reads := files.reads.Load()
	t.Cleanup(func() {
		if files.reads.Load() != reads {
			t.Fatal("request handling reread templates")
		}
	})
	for _, mode := range []string{"ordinary", "HTMX", "history"} {
		for _, method := range []string{"GET", "HEAD"} {
			for _, kind := range []string{"message", "error", "missing"} {
				t.Run(mode+"/"+method+"/"+kind, func(t *testing.T) {
					t.Parallel()
					var logs bytes.Buffer
					r := httptest.NewRequest(method, "/", nil)
					r = r.WithContext(logger.Set(r.Context(), slog.New(slog.NewTextHandler(&logs, nil))))
					if mode != "ordinary" {
						r.Header.Set("HX-Request", "true")
					}
					if mode == "history" {
						r.Header.Set("HX-History-Restore-Request", "true")
					}
					w := httptest.NewRecorder()
					w.Header().Set("Cache-Control", "public")
					status, want := 200, "message:&lt;script&gt;"
					switch kind {
					case "message":
						cm.HandleMessage(w, r, "<script>")
					case "error":
						status, want = 422, "error:&lt;script&gt;"
						cm.HandleError(w, r, "<script>", errors.New("private diagnostic"), status)
					case "missing":
						status, want = 404, "missing:Not Found"
						cm.HandleNotFound(w, r)
					}
					if mode != "HTMX" {
						want = "<main>" + want + "</main>"
					}
					if method == "HEAD" {
						want = ""
					}
					if w.Code != status || w.Body.String() != want || w.Header().Get("Cache-Control") != "no-store" {
						t.Fatalf("response = %d %v %q, want %d %q", w.Code, w.Header(), w.Body, status, want)
					}
					vary := strings.Join(w.Header().Values("Vary"), ",")
					if !strings.Contains(vary, "HX-Request") || !strings.Contains(vary, "HX-History-Restore-Request") {
						t.Fatal("missing response variants", vary)
					}
					if strings.Count(logs.String(), "private diagnostic") != map[bool]int{true: 1, false: 0}[kind == "error"] {
						t.Fatal("unexpected logging", logs.String())
					}
				})
			}
		}
	}
}

func TestConfiguredResponsesReplaceBodyHeaders(t *testing.T) {
	cm, files := responseManager()
	if err := ConfigureResponses(cm, responseSelection(), TemplatePath{FS: files, Path: "responses.html"}); err != nil {
		t.Fatal(err)
	}
	if err := cm.PrepareTemplates(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A handler chose another representation, then delegated its response.
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "1")
		switch r.URL.Path {
		case "/message":
			cm.HandleMessage(w, r, "hello")
		case "/error":
			cm.HandleError(w, r, "unavailable", nil, http.StatusServiceUnavailable)
		default:
			cm.HandleNotFound(w, r)
		}
	}))
	defer server.Close()

	for _, tc := range []struct {
		path, body string
		status     int
	}{
		{"/message", "<main>message:hello</main>", http.StatusOK},
		{"/error", "<main>error:unavailable</main>", http.StatusServiceUnavailable},
		{"/missing", "<main>missing:Not Found</main>", http.StatusNotFound},
	} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			t.Run(method+tc.path, func(t *testing.T) {
				r, err := http.NewRequestWithContext(t.Context(), method, server.URL+tc.path, nil)
				if err != nil {
					t.Fatal(err)
				}
				response, err := server.Client().Do(r)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				body, err := io.ReadAll(response.Body)
				wantBody := tc.body
				if method == http.MethodHead {
					wantBody = ""
				}
				if err != nil || response.StatusCode != tc.status || string(body) != wantBody {
					t.Fatalf("response=%d %q, read error=%v", response.StatusCode, body, err)
				}
				if response.Header.Get("Content-Type") != "text/html; charset=utf-8" || response.Header.Get("Content-Length") == "1" {
					t.Fatal("response retained headers for the discarded body", response.Header)
				}
			})
		}
	}
}

func TestResponseConfigurationValidation(t *testing.T) {
	if err := ConfigureResponses(nil, responseSelection()); err == nil {
		t.Fatal("accepted nil manager")
	}
	for _, missing := range []string{"message", "error", "not found"} {
		t.Run(missing, func(t *testing.T) {
			cm, _ := responseManager()
			selection := responseSelection()
			switch missing {
			case "message":
				selection.Message = ""
			case "error":
				selection.Error = ""
			case "not found":
				selection.NotFound = ""
			}
			if err := ConfigureResponses(cm, selection); err == nil || cm.PrepareTemplates != nil || cm.HandleError != nil {
				t.Fatal("invalid selection changed configuration")
			}
		})
	}
	cm, _ := responseManager()
	sentinel := errors.New("existing preparation")
	cm.PrepareTemplates = func() error { return sentinel }
	if err := ConfigureResponses(cm, responseSelection()); err == nil || cm.PrepareTemplates() != sentinel {
		t.Fatal("replaced an existing preparation callback")
	}
}

func TestResponsePreparationAndOverrides(t *testing.T) {
	for _, mode := range []string{"missing file", "syntax", "unknown partial", "fragment only", "override"} {
		t.Run(mode, func(t *testing.T) {
			cm, files := responseManager()
			paths := []TemplatePath{{FS: files, Path: "responses.html"}}
			selection := responseSelection()
			switch mode {
			case "missing file":
				paths[0].Path = "absent.html"
			case "syntax":
				files.FS.(fstest.MapFS)["responses.html"].Data = []byte(`{{if}}`)
			case "unknown partial":
				selection.Error = "absent"
			case "fragment only":
				selection.Error = "fragment-error"
				files.FS.(fstest.MapFS)["fragment.html"].Data = []byte(`{{renderPartial .PartialName .}}{{define "fragment-error"}}error{{end}}`)
			}
			if err := ConfigureResponses(cm, selection, paths...); err != nil {
				t.Fatal(err)
			}
			if mode == "override" {
				// File selections are copied, but layout overrides apply at startup.
				paths[0].Path = "absent.html"
				cm.RegisterLayouts(contentservice.PageType, fstest.MapFS{"override.html": {Data: []byte(`override:{{renderPartial .PartialName .}}`)}}, "override.html")
				cm.HandleNotFound = func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(410) }
			}
			err := cm.PrepareTemplates()
			if mode != "override" {
				if err == nil {
					t.Fatal("invalid templates passed startup")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			cm.HandleMessage(w, httptest.NewRequest("GET", "/", nil), "hello")
			if w.Body.String() != "override:message:hello" {
				t.Fatal("layout override was ignored", w.Body)
			}
			w = httptest.NewRecorder()
			cm.HandleNotFound(w, httptest.NewRequest("GET", "/", nil))
			if w.Code != 410 {
				t.Fatal("preparation replaced a custom handler")
			}
		})
	}
}

func TestResponseErrorStatusesAndNotifications(t *testing.T) {
	cm, files := responseManager()
	selection := responseSelection()
	selection.NotFoundMessage = "Nothing here"
	if err := ConfigureResponses(cm, selection, TemplatePath{FS: files, Path: "responses.html"}); err != nil {
		t.Fatal(err)
	}
	if err := cm.PrepareTemplates(); err != nil {
		t.Fatal(err)
	}
	loads := 0
	cm.LoadNotifications = func(*http.Request) ([]messages.Notification, func(http.ResponseWriter) error, error) {
		loads++
		return nil, nil, errors.New("notification store unavailable")
	}
	for _, status := range []int{0, 200, 302, 399, 400, 404, 599, 600} {
		w := httptest.NewRecorder()
		cm.HandleError(w, httptest.NewRequest("GET", "/", nil), "", nil, status)
		want := status
		if status < 400 || status > 599 {
			want = 500
		}
		if w.Code != want || w.Body.String() != "<main>error:"+http.StatusText(want)+"</main>" || loads != 0 {
			t.Fatalf("status %d: %d %s, loads=%d", status, w.Code, w.Body, loads)
		}
	}
	w := httptest.NewRecorder()
	cm.HandleNotFound(w, httptest.NewRequest("GET", "/", nil))
	if w.Body.String() != "<main>missing:Nothing here</main>" || loads != 0 {
		t.Fatal("not-found response depended on notifications", w.Body)
	}
	w = httptest.NewRecorder()
	cm.HandleMessage(w, httptest.NewRequest("GET", "/", nil), "hello")
	if w.Code != 500 || loads != 1 {
		t.Fatal("message response did not use the configured notification loader")
	}
}

func TestResponseFailureFallback(t *testing.T) {
	for _, mode := range []string{"not prepared", "execution", "notification", "delivery"} {
		for _, method := range []string{"GET", "HEAD"} {
			t.Run(mode+"/"+method, func(t *testing.T) {
				cm, files := responseManager()
				if mode == "execution" {
					files.FS.(fstest.MapFS)["responses.html"].Data = []byte(`{{define "message"}}partial output{{index .Data 999}}{{end}}{{define "error"}}error{{end}}{{define "missing"}}missing{{end}}`)
				}
				if err := ConfigureResponses(cm, responseSelection(), TemplatePath{FS: files, Path: "responses.html"}); err != nil {
					t.Fatal(err)
				}
				if mode != "not prepared" {
					if err := cm.PrepareTemplates(); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "notification" {
					cm.LoadNotifications = func(*http.Request) ([]messages.Notification, func(http.ResponseWriter) error, error) {
						return nil, func(http.ResponseWriter) error { return errors.New("acknowledgment failed") }, nil
					}
				}
				cm.HandleError = func(http.ResponseWriter, *http.Request, string, error, int) { t.Fatal("recursive fallback") }
				w := &responseFailureWriter{ResponseRecorder: httptest.NewRecorder(), fail: mode == "delivery"}
				if mode == "not prepared" || mode == "execution" {
					w.Header().Set("Content-Length", "777")
				}
				var logs bytes.Buffer
				r := httptest.NewRequest(method, "/", nil)
				r = r.WithContext(logger.Set(r.Context(), slog.New(slog.NewTextHandler(&logs, nil))))
				cm.HandleMessage(w, r, "hello")
				wantStatus := 500
				if mode == "delivery" || mode == "notification" && method == "HEAD" {
					wantStatus = 200
				}
				if w.Code != wantStatus || w.statusWrites != 1 || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("response = %d, status writes=%d, headers=%v", w.Code, w.statusWrites, w.Header())
				}
				if method == "HEAD" && w.bodyWrites != 0 {
					t.Fatal("HEAD attempted to write a body")
				}
				if method == "GET" && mode != "delivery" && (w.Body.String() != "Unable to display this page.\n" || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain")) {
					t.Fatal("fallback leaked partial output", w.Body)
				}
				if mode == "delivery" && method == "GET" && w.bodyWrites != 1 {
					t.Fatal("delivery failure attempted a second response")
				}
				if wantStatus == 500 && w.Header().Get("Content-Length") != "" {
					t.Fatal("fallback retained a stale content length")
				}
				if (wantStatus == 500 || mode == "delivery" && method == "GET") && logs.Len() == 0 {
					t.Fatal("response failure was not logged")
				}
			})
		}
	}
}

type responseFailureWriter struct {
	*httptest.ResponseRecorder
	fail                     bool
	statusWrites, bodyWrites int
}

func (w *responseFailureWriter) WriteHeader(status int) {
	w.statusWrites++
	w.ResponseRecorder.WriteHeader(status)
}

func (w *responseFailureWriter) Write(data []byte) (int, error) {
	w.bodyWrites++
	if w.fail {
		return 0, fmt.Errorf("connection lost")
	}
	return w.ResponseRecorder.Write(data)
}
