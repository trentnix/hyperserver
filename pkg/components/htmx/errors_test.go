package htmx

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestServeErrorScript(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /custom/errors.js", ServeErrorScript)
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest(method, "/custom/errors.js", nil))
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d", w.Code)
			}
			for name, want := range map[string]string{
				"Content-Type":           "text/javascript; charset=utf-8",
				"Cache-Control":          "no-cache",
				"X-Content-Type-Options": "nosniff",
				"Content-Length":         strconv.Itoa(len(errorScript)),
			} {
				if got := w.Header().Get(name); got != want {
					t.Errorf("%s = %q, want %q", name, got, want)
				}
			}
			want := errorScript
			if method == http.MethodHead {
				want = ""
			}
			if w.Body.String() != want {
				t.Fatal("unexpected script body")
			}
		})
	}
}
