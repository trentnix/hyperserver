package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"runtime"
	"strings"
	"testing"
)

// performanceRequests uses the HTTP fixture's isolated SQLite storage and fake
// mail. It excludes setup, password hashing, sockets, TLS, and browser execution.
// Logging still formats every request, but does not retain the log output.
func performanceRequests(t testing.TB) (http.Handler, []*http.Request) {
	t.Helper()
	h := newHTTPHarness(t)
	handler, err := applicationHandler(h.app, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	base, err := url.Parse(h.baseURL)
	if err != nil {
		t.Fatal(err)
	}
	var requests []*http.Request
	for _, authenticated := range []bool{false, true} {
		if authenticated {
			h.establishSession(t, h.seedUser(t, "benchmark@example.invalid"))
		}
		for _, fragment := range []bool{false, true} {
			r := httptest.NewRequest(http.MethodGet, h.baseURL+"/", nil)
			if fragment {
				r.Header.Set("HX-Request", "true")
			}
			for _, cookie := range h.cookies.Cookies(base) {
				r.AddCookie(cookie)
			}
			requests = append(requests, r)
		}
	}
	for i, r := range requests {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r.Clone(r.Context()))
		body := w.Body.String()
		if w.Code != http.StatusOK || strings.Contains(body, "benchmark@example.invalid") != (i >= 2) || strings.Contains(body, "<!DOCTYPE html>") != (i%2 == 0) {
			t.Fatalf("reference workload %d did not render the expected identity and representation", i)
		}
	}
	return handler, requests
}

func servePerformanceRequest(t testing.TB, handler http.Handler, r *http.Request) bool {
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r.Clone(r.Context()))
	if w.Code != http.StatusOK || w.Body.Len() == 0 {
		t.Errorf("reference request returned %d with %d bytes", w.Code, w.Body.Len())
		return false
	}
	return true
}

func BenchmarkReferenceRequest(b *testing.B) {
	for i, name := range []string{"anonymous-page", "anonymous-fragment", "authenticated-page", "authenticated-fragment"} {
		b.Run(name, func(b *testing.B) {
			handler, requests := performanceRequests(b)
			b.ReportAllocs()
			for b.Loop() {
				if !servePerformanceRequest(b, handler, requests[i]) {
					return
				}
			}
		})
	}
}

// BenchmarkReferenceLoad mixes all four request types evenly across GOMAXPROCS
// workers. ns/op is throughput-normalized cost, not a latency percentile.
func BenchmarkReferenceLoad(b *testing.B) {
	handler, requests := performanceRequests(b)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if !servePerformanceRequest(b, handler, requests[i%len(requests)]) {
				return
			}
			i++
		}
	})
}

// TestReferenceHeapBudget checks retained Go heap, not allocation traffic or RSS.
// Opt in on an otherwise idle machine. Keep the application live during both GCs.
func TestReferenceHeapBudget(t *testing.T) {
	if os.Getenv("HS_TEST_PERFORMANCE") != "1" {
		t.Skip("set HS_TEST_PERFORMANCE=1 for the retained-heap budget")
	}
	handler, requests := performanceRequests(t)
	for i := range 100 {
		if !servePerformanceRequest(t, handler, requests[i%len(requests)]) {
			return
		}
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := range 10000 {
		if !servePerformanceRequest(t, handler, requests[i%len(requests)]) {
			return
		}
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(handler)
	runtime.KeepAlive(requests)
	growth := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	t.Logf("retained Go heap: before=%d after=%d growth=%d bytes after 10000 requests", before.HeapAlloc, after.HeapAlloc, growth)
	const maxGrowth = 1024 * 1024
	if growth > maxGrowth {
		t.Fatalf("retained heap growth %d exceeds %d bytes", growth, maxGrowth)
	}
}
