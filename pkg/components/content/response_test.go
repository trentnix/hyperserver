package content

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/trentnix/hyperserver/pkg/components/htmx"
	"github.com/trentnix/hyperserver/pkg/components/messages"
	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
)

func TestResponseHTMXMetadata(t *testing.T) {
	for _, tc := range []struct {
		name     string
		metadata htmx.Response
		header   string
		value    string
	}{
		{"push", htmx.Response{PushURL: "/next"}, "HX-Push-Url", "/next"},
		{"replace", htmx.Response{ReplaceURL: "/next"}, "HX-Replace-Url", "/next"},
		{"refresh", htmx.Response{Refresh: true}, "HX-Refresh", "true"},
		{"trigger", htmx.Response{Trigger: `{"saved":{"id":42}}`}, "HX-Trigger", `{"saved":{"id":42}}`},
		{"after swap", htmx.Response{TriggerAfterSwap: "saved"}, "HX-Trigger-After-Swap", "saved"},
		{"after settle", htmx.Response{TriggerAfterSettle: "saved"}, "HX-Trigger-After-Settle", "saved"},
		{"retarget", htmx.Response{Retarget: "#editor"}, "HX-Retarget", "#editor"},
		{"reswap", htmx.Response{Reswap: "outerHTML"}, "HX-Reswap", "outerHTML"},
	} {
		for _, hx := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/htmx=%t", tc.name, hx), func(t *testing.T) {
				r := httptest.NewRequest(http.MethodGet, "/", nil)
				r.Header.Set(htmx.HeaderRequest, fmt.Sprint(hx))
				c := NewContent(nil) // Dispatch must classify the actual request.
				c.AddContent(fstest.MapFS{"view.html": {Data: []byte("<p>body</p>")}}, "view.html")
				c.HTMX.Response = &tc.metadata
				w := httptest.NewRecorder()
				if err := c.Render(w, r); err != nil {
					t.Fatal(err)
				}
				want := ""
				if hx {
					want = tc.value
				}
				if got := w.Result().Header.Get(tc.header); got != want {
					t.Fatalf("%s = %q, want %q", tc.header, got, want)
				}
				if w.Code != http.StatusOK || w.Body.String() != "<p>body</p>" || w.Result().Header.Get("Content-Type") != "text/html; charset=utf-8" {
					t.Fatalf("response = %d %v %q", w.Code, w.Result().Header, w.Body.String())
				}
			})
		}
	}
}

func TestResponseOutOfBandAndNotifications(t *testing.T) {
	files := fstest.MapFS{
		"page.html":     {Data: []byte(`<html>{{renderPartial "form" .}}</html>`)},
		"fragment.html": {Data: []byte(`{{renderPartial "form" .}}`)},
		"parts.html":    {Data: []byte(`{{define "form"}}<form>{{.Data}}</form>{{end}}{{define "notice"}}<div id="notice" hx-swap-oob="true">{{range .Notifications}}{{.Message}}|{{end}}</div>{{end}}{{define "count"}}<span id="count" hx-swap-oob="outerHTML">2</span>{{end}}`)},
	}
	for _, hx := range []bool{false, true} {
		t.Run(fmt.Sprintf("htmx=%t", hx), func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/", nil)
			r.Header.Set(htmx.HeaderRequest, fmt.Sprint(hx))
			cm := contentservice.NewContentManager()
			cm.AddPageLayout(files, "page.html")
			cm.AddHtmxLayout(files, "fragment.html")
			calls := 0
			cm.LoadNotifications = func(http.ResponseWriter, *http.Request) ([]messages.Notification, error) {
				calls++
				return []messages.Notification{messages.NewNotification("<loaded>", messages.NotificationTypeSuccess)}, nil
			}
			c := NewManagedContent(nil, cm)
			c.AddContent(files, "parts.html")
			c.Data = "<invalid input>"
			c.Notifications = []messages.Notification{messages.NewNotification("<local>", messages.NotificationTypeError)}
			c.OutOfBand = []string{"notice", "count"}
			c.HTMX.Response = &htmx.Response{Retarget: "#form", Reswap: "outerHTML"}
			// Reusing the description must not accumulate loaded notifications.
			for range 2 {
				w := httptest.NewRecorder()
				if err := c.Render(w, r); err != nil {
					t.Fatal(err)
				}
				want := "<html><form>&lt;invalid input&gt;</form></html>"
				if hx {
					want = `<form>&lt;invalid input&gt;</form><div id="notice" hx-swap-oob="true">&lt;local&gt;|&lt;loaded&gt;|</div><span id="count" hx-swap-oob="outerHTML">2</span>`
				}
				if w.Body.String() != want || w.Code != http.StatusOK {
					t.Fatalf("response = %d %q, want %q", w.Code, w.Body.String(), want)
				}
			}
			if calls != 2 || len(c.Notifications) != 1 {
				t.Fatalf("loader calls = %d, original notifications = %v", calls, c.Notifications)
			}
		})
	}
}

func TestResponseOutOfBandFailureDoesNotCommit(t *testing.T) {
	for _, name := range []string{"missing", "failure"} {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.Header.Set(htmx.HeaderRequest, "true")
			c := NewContent(r)
			c.AddContent(fstest.MapFS{"view.html": {Data: []byte(`<p>main</p>{{define "failure"}}<p>prefix</p>{{.Data.Fail}}{{end}}`)}}, "view.html")
			c.Data = failingTemplateData{err: errors.New("template failure")}
			c.OutOfBand = []string{name}
			c.Headers = map[string]string{"X-Success": "yes"}
			c.HTMX.Response = &htmx.Response{Trigger: "saved", Retarget: "#form"}
			w := &recordingResponse{ResponseRecorder: httptest.NewRecorder()}
			if err := c.Render(w, r); err == nil {
				t.Fatal("expected out-of-band rendering error")
			}
			if w.committed || w.Body.Len() != 0 || len(w.Header()) != 0 {
				t.Fatalf("failed render committed response: %v %q", w.Header(), w.Body.String())
			}
		})
	}
}

func TestResponseRedirect(t *testing.T) {
	for _, hx := range []bool{false, true} {
		for _, destination := range []string{"/saved?from=form", "https://outside.invalid/", "//outside.invalid/", "/%0d%0aX-Evil:yes"} {
			t.Run(fmt.Sprintf("%s/htmx=%t", destination, hx), func(t *testing.T) {
				r := httptest.NewRequest(http.MethodPost, "/", nil)
				r.Header.Set(htmx.HeaderRequest, fmt.Sprint(hx))
				cm := contentservice.NewContentManager()
				cm.LoadNotifications = func(http.ResponseWriter, *http.Request) ([]messages.Notification, error) {
					t.Fatal("redirect consumed notifications")
					return nil, nil
				}
				c := NewManagedContent(r, cm)
				c.RedirectURL = destination
				c.OutOfBand = []string{"missing"}
				c.HTMX.Response = &htmx.Response{Trigger: "ignored"}
				w := httptest.NewRecorder()
				if err := c.Render(w, r); err != nil {
					t.Fatal(err)
				}
				status, header, absent := http.StatusSeeOther, "Location", "HX-Redirect"
				if hx {
					status, header, absent = http.StatusOK, "HX-Redirect", "Location"
				}
				want := "/"
				if strings.HasPrefix(destination, "/saved") {
					want = destination
				}
				h := w.Result().Header
				if w.Code != status || h.Get(header) != want || h.Get(absent) != "" || h.Get("HX-Trigger") != "" || w.Body.Len() != 0 || h.Get("Vary") != "HX-Request" || h.Get("Cache-Control") != "no-store" {
					t.Fatalf("redirect = %d %v %q", w.Code, h, w.Body.String())
				}
			})
		}
	}
}

func TestResponseNoContent(t *testing.T) {
	for _, hx := range []bool{false, true} {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.Header.Set(htmx.HeaderRequest, fmt.Sprint(hx))
		cm := contentservice.NewContentManager()
		calls := 0
		cm.LoadNotifications = func(http.ResponseWriter, *http.Request) ([]messages.Notification, error) {
			calls++
			return nil, nil
		}
		c := NewManagedContent(r, cm)
		c.AddContent(fstest.MapFS{"view.html": {Data: []byte("ordinary page")}}, "view.html")
		c.HTMX.Response = &htmx.Response{NoContent: true, Trigger: "saved"}
		w := httptest.NewRecorder()
		if err := c.Render(w, r); err != nil {
			t.Fatal(err)
		}
		if hx {
			if w.Code != http.StatusNoContent || w.Body.Len() != 0 || calls != 0 || w.Result().Header.Get("HX-Trigger") != "saved" {
				t.Fatalf("bodyless response = %d %v %q, loader calls = %d", w.Code, w.Result().Header, w.Body.String(), calls)
			}
		} else if w.Code != http.StatusOK || w.Body.String() != "ordinary page" || calls != 1 || w.Result().Header.Get("HX-Trigger") != "" {
			t.Fatalf("ordinary response = %d %v %q, loader calls = %d", w.Code, w.Result().Header, w.Body.String(), calls)
		}
	}
}

func TestResponseRejectsInvalidStatus(t *testing.T) {
	for _, status := range []int{-1, 100, 199, 205, 301, 304, 399, 600} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			c := NewContent(r)
			c.ResponseStatusCode = status
			w := &recordingResponse{ResponseRecorder: httptest.NewRecorder()}
			if err := c.Render(w, r); err == nil || w.committed || len(w.Header()) != 0 {
				t.Fatalf("invalid status result: err=%v, committed=%t, headers=%v", err, w.committed, w.Header())
			}
		})
	}
}

func TestResponseHead(t *testing.T) {
	r := httptest.NewRequest(http.MethodHead, "/", nil)
	c := NewContent(r)
	c.AddContent(fstest.MapFS{"view.html": {Data: []byte("<p>body</p>")}}, "view.html")
	w := httptest.NewRecorder()
	if err := c.Render(w, r); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || w.Body.Len() != 0 || w.Result().Header.Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("HEAD response = %d %v %q", w.Code, w.Result().Header, w.Body.String())
	}
}

func TestResponseExplicitNoContent(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	c := NewContent(r)
	c.ResponseStatusCode = http.StatusNoContent
	c.Headers = map[string]string{"X-Saved": "true"}
	w := httptest.NewRecorder()
	if err := c.Render(w, r); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusNoContent || w.Body.Len() != 0 || w.Result().Header.Get("X-Saved") != "true" {
		t.Fatalf("204 response = %d %v %q", w.Code, w.Result().Header, w.Body.String())
	}
}
