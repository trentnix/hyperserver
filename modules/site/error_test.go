package module_site

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/trentnix/hyperserver/pkg/components/content"
	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
)

func TestQueuedErrorAcknowledgment(t *testing.T) {
	for _, hx := range []bool{false, true} {
		for _, outcome := range []string{"success", "HEAD", "template failure", "acknowledgment failure"} {
			t.Run(fmt.Sprintf("htmx=%t/%s", hx, outcome), func(t *testing.T) {
				cm := contentservice.NewContentManager()
				source := "rendered error"
				if outcome == "template failure" {
					source += `{{index .Data 99}}`
				}
				files := fstest.MapFS{"error.html": {Data: []byte(source)}}
				prepared, err := content.PrepareTemplates(cm, content.TemplatePath{FS: files, Path: "error.html"})
				if err != nil {
					t.Fatal(err)
				}
				renderer := pageRenderer{contentManager: cm, errorTemplates: prepared}
				r := httptest.NewRequest(http.MethodGet, "/error", nil)
				r.Header.Set("HX-Request", fmt.Sprint(hx))
				if outcome == "HEAD" {
					r.Method = http.MethodHead
				}
				w := httptest.NewRecorder()
				calls := 0
				renderer.renderError(w, r, nil, http.StatusOK, func(writer http.ResponseWriter) error {
					calls++
					if w.Body.Len() != 0 || writer != w {
						t.Fatal("acknowledgment ran after delivery or received the staging buffer")
					}
					if outcome == "acknowledgment failure" {
						return errors.New("acknowledgment unavailable")
					}
					writer.Header().Add("Set-Cookie", "notice=; Max-Age=0")
					return nil
				})
				wantCalls := 0
				if outcome == "success" || outcome == "acknowledgment failure" {
					wantCalls = 1
				}
				if calls != wantCalls {
					t.Fatalf("acknowledgments=%d, want %d", calls, wantCalls)
				}
				if strings.HasSuffix(outcome, "failure") && (w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "rendered error") || len(w.Result().Cookies()) != 0) {
					t.Fatalf("failure committed staged response: %d %v %q", w.Code, w.Header(), w.Body.String())
				}
				if outcome == "success" && (w.Code != http.StatusOK || w.Body.String() != "rendered error" || len(w.Result().Cookies()) != 1) {
					t.Fatal("successful error response lost acknowledgment or content")
				}
				if outcome == "HEAD" && (w.Code != http.StatusOK || w.Body.Len() != 0 || len(w.Result().Cookies()) != 0) {
					t.Fatal("HEAD wrote content or acknowledged notifications")
				}
			})
		}
	}
}
