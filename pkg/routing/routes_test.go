package routing

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRouteBodyLimits(t *testing.T) {
	mux := http.NewServeMux()
	routes := NewRoutes(mux)
	read := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.Copy(io.Discard, r.Body)
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "Body too large", http.StatusRequestEntityTooLarge)
			return
		}
		if err != nil {
			t.Errorf("read: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	routes.Handle("POST /default", read)
	routes.HandleFunc("POST /function", read)
	routes.HandleWithBodyLimit("POST /larger", read, 2*DefaultMaxBodyBytes)
	routes.HandleWithBodyLimit("POST /small", read, 5)
	routes.HandleWithBodyLimit("POST /zero", read, 0)
	routes.HandleWithBodyLimit("POST /negative", read, -1)
	routes.HandleWithoutBodyLimit("POST /unlimited", read)

	for _, tc := range []struct {
		path   string
		size   int
		status int
	}{
		{"/default", int(DefaultMaxBodyBytes), 204},
		{"/default", int(DefaultMaxBodyBytes) + 1, 413},
		{"/function", int(DefaultMaxBodyBytes) + 1, 413},
		{"/larger", int(DefaultMaxBodyBytes) + 1, 204},
		{"/larger", 2*int(DefaultMaxBodyBytes) + 1, 413},
		{"/small", 5, 204},
		{"/small", 6, 413},
		{"/zero", 0, 204},
		{"/zero", 1, 413},
		{"/negative", 1, 413},
		{"/unlimited", 2*int(DefaultMaxBodyBytes) + 1, 204},
	} {
		t.Run(tc.path, func(t *testing.T) {
			for _, contentType := range []string{"", "application/json", "multipart/form-data; boundary=test", "application/octet-stream"} {
				for _, unknownLength := range []bool{false, true} {
					r := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(strings.Repeat("x", tc.size)))
					r.Header.Set("Content-Type", contentType)
					if unknownLength {
						r.ContentLength = -1
					}
					w := httptest.NewRecorder()
					mux.ServeHTTP(w, r)
					if w.Code != tc.status {
						t.Fatalf("type=%q unknown length=%t size=%d: status=%d, want %d", contentType, unknownLength, tc.size, w.Code, tc.status)
					}
				}
			}
		})
	}
}

type bodyCloseProbe struct {
	io.Reader
	closed bool
}

func (b *bodyCloseProbe) Close() error { b.closed = true; return nil }

func TestRouteBodyLimitPreservesCloseAndDoesNotReadAhead(t *testing.T) {
	source := strings.NewReader("body")
	body := &bodyCloseProbe{Reader: source}
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Body = body
	mux := http.NewServeMux()
	NewRoutes(mux).HandleFunc("POST /", func(w http.ResponseWriter, request *http.Request) {
		if request != r {
			t.Error("body limit copied the request")
		}
		if source.Len() != 4 {
			t.Error("body was read before reaching the handler")
		}
		request.Body.Close()
	})
	mux.ServeHTTP(httptest.NewRecorder(), r)
	if !body.closed {
		t.Fatal("underlying body was not closed")
	}
}
