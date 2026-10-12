package htmx

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestGetRequest(t *testing.T) {
	for _, value := range []string{"", "true", "false", "TRUE", "1", " true "} {
		t.Run(value, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			for _, header := range []string{"hx-request", "hx-boosted", "hx-history-restore-request"} {
				r.Header.Set(header, value)
			}
			r.Header.Set("HX-Trigger", "save-button")
			r.Header.Set("HX-Trigger-Name", "save")
			r.Header.Set("HX-Target", "editor")
			r.Header.Set("HX-Prompt", "A&B + café")
			want := Request{
				Enabled: value == "true", Boosted: value == "true", HistoryRestore: value == "true",
				Trigger: "save-button", TriggerName: "save", Target: "editor", Prompt: "A&B + café",
			}
			if got := GetRequest(r); got != want {
				t.Fatalf("GetRequest = %+v, want %+v", got, want)
			}
			if IsHtmxRequest(r) != want.Enabled {
				t.Fatal("request classification disagrees with GetRequest")
			}
		})
	}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if got := GetRequest(r); got != (Request{}) {
		t.Fatalf("ordinary request = %+v", got)
	}
	// Boosted and history headers must not independently classify a request as HTMX.
	for _, header := range []string{HeaderRequest, HeaderBoosted, HeaderHistoryRestoreRequest} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set(header, "true")
		got := GetRequest(r)
		if got.Enabled != (header == HeaderRequest) || got.Boosted != (header == HeaderBoosted) || got.HistoryRestore != (header == HeaderHistoryRestoreRequest) {
			t.Fatalf("independent header %s: %+v", header, got)
		}
	}
}

type recordingWriter struct {
	*httptest.ResponseRecorder
	statuses []int
}

func (w *recordingWriter) WriteHeader(status int) {
	w.statuses = append(w.statuses, status)
	w.ResponseRecorder.WriteHeader(status)
}

func TestResponseApply(t *testing.T) {
	for _, noContent := range []bool{false, true} {
		name := "headers only"
		if noContent {
			name = "no content"
		}
		t.Run(name, func(t *testing.T) {
			response := Response{
				PushURL: "/next?q=1", ReplaceURL: "/current", Refresh: true,
				Retarget: "#editor", Reswap: "outerHTML settle:0",
				Trigger: `{"saved":{"id":42}}`, TriggerAfterSwap: "updated", TriggerAfterSettle: "ready",
				NoContent: noContent,
			}
			original := response
			w := &recordingWriter{ResponseRecorder: httptest.NewRecorder()}
			w.Header().Set("X-Unrelated", "preserved")
			w.Header().Set("HX-Retarget", "#old-target")
			response.Apply(w)

			want := http.Header{}
			for key, value := range map[string]string{
				"HX-Push-Url": "/next?q=1", "HX-Replace-Url": "/current", "HX-Refresh": "true",
				"HX-Retarget": "#editor", "HX-Reswap": "outerHTML settle:0",
				"HX-Trigger": `{"saved":{"id":42}}`, "HX-Trigger-After-Swap": "updated", "HX-Trigger-After-Settle": "ready",
				"X-Unrelated": "preserved",
			} {
				want.Set(key, value)
			}
			// Result captures headers at the first commit, including the 204 path.
			if got := w.Result().Header; !reflect.DeepEqual(got, want) {
				t.Fatalf("headers = %v, want %v", got, want)
			}
			if noContent {
				if !reflect.DeepEqual(w.statuses, []int{http.StatusNoContent}) {
					t.Fatalf("status writes = %v", w.statuses)
				}
			} else if len(w.statuses) != 0 {
				t.Fatalf("metadata committed status: %v", w.statuses)
			}
			if w.Body.Len() != 0 || response != original {
				t.Fatal("Apply wrote a body or mutated its metadata")
			}
		})
	}
}

func TestEmptyResponsePreservesHeaders(t *testing.T) {
	w := &recordingWriter{ResponseRecorder: httptest.NewRecorder()}
	for _, header := range []string{HeaderPushURL, HeaderReplaceURL, HeaderRefresh, HeaderRetarget, HeaderReswap, HeaderTrigger, HeaderTriggerAfterSwap, HeaderTriggerAfterSettle, "X-Unrelated"} {
		w.Header().Set(header, "existing")
	}
	want := w.Header().Clone()
	Response{}.Apply(w)
	if !reflect.DeepEqual(w.Header(), want) || len(w.statuses) != 0 || w.Body.Len() != 0 {
		t.Fatalf("empty response changed output: headers=%v statuses=%v body=%q", w.Header(), w.statuses, w.Body.String())
	}
}

func TestNewResponseIsIndependent(t *testing.T) {
	first, second := NewResponse(), NewResponse()
	if first == second || *first != (Response{}) || *second != (Response{}) {
		t.Fatal("NewResponse must return independent zero-valued metadata")
	}
	first.Retarget = "#editor"
	if second.Retarget != "" {
		t.Fatal("responses share state")
	}
}
