package util

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHttpErrorHidesInternalDetails(t *testing.T) {
	for _, internal := range []error{nil, fmt.Errorf("query failed: %w", errors.New("private database details"))} {
		for _, status := range []int{http.StatusBadRequest, http.StatusInternalServerError} {
			for _, htmx := range []bool{false, true} {
				r := httptest.NewRequest(http.MethodGet, "/", nil)
				if htmx {
					r.Header.Set("HX-Request", "true")
				}
				w := httptest.NewRecorder()
				const message = `<script>alert("public text")</script>`
				HttpError(w, r, message, internal, status)
				if w.Code != status || w.Body.String() != message+"\n" {
					t.Fatalf("status=%d body=%q, want %d and only the public message", w.Code, w.Body.String(), status)
				}
				if w.Header().Get("Content-Type") != "text/plain; charset=utf-8" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
					t.Fatalf("error message was not served safely as text: %v", w.Header())
				}
			}
		}
	}
}
