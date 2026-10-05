package ratelimit

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func testRateLimiter(t *testing.T, requests int, window time.Duration, capacity int) *Limiter {
	t.Helper()
	l, err := New("test", Policy{Requests: requests, Window: window, MaxClients: capacity})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func rateRequest(h http.Handler, address string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.RemoteAddr = address
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestRateLimiterValidation(t *testing.T) {
	for _, tc := range []struct {
		requests int
		window   time.Duration
		capacity int
	}{
		{0, time.Second, 1}, {-1, time.Second, 1},
		{1, 0, 1}, {1, -time.Second, 1},
		{1, time.Second, 0}, {1, time.Second, -1},
	} {
		if _, err := New("test", Policy{Requests: tc.requests, Window: tc.window, MaxClients: tc.capacity}); err == nil {
			t.Errorf("accepted invalid limits: %+v", tc)
		}
	}
	for _, l := range []*Limiter{nil, {}} {
		h := l.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("unconfigured limiter called handler") }))
		if w := rateRequest(h, "192.0.2.1:1000"); w.Code != 500 {
			t.Fatalf("unconfigured status=%d", w.Code)
		}
	}
}

func TestRateLimiterWindowAndSharedRoutes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		l := testRateLimiter(t, 2, 1500*time.Millisecond, 10)
		calls := 0
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.WriteHeader(204)
		})
		first, second := l.Handler(next), l.Handler(next)
		for _, h := range []http.Handler{first, second} {
			if w := rateRequest(h, "192.0.2.1:1000"); w.Code != 204 {
				t.Fatal("request inside budget denied")
			}
		}
		w := rateRequest(first, "192.0.2.1:2000")
		if w.Code != 429 || w.Header().Get("Retry-After") != "2" || w.Header().Get("Cache-Control") != "no-store" || calls != 2 {
			t.Fatalf("exhausted response: %d %v calls=%d", w.Code, w.Header(), calls)
		}
		time.Sleep(1499 * time.Millisecond)
		if w := rateRequest(second, "192.0.2.1:1000"); w.Code != 429 || w.Header().Get("Retry-After") != "1" {
			t.Fatal("window ended early or retry delay was rounded down")
		}
		time.Sleep(time.Millisecond)
		w = rateRequest(second, "192.0.2.1:1000")
		if w.Code != 204 || calls != 3 || w.Header().Get("Retry-After") != "" {
			t.Fatal("denied requests extended the window")
		}
	})
}

func TestRateLimiterClientIdentity(t *testing.T) {
	l := testRateLimiter(t, 1, time.Minute, 10)
	h := l.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for _, tc := range []struct {
		address string
		status  int
	}{
		{"192.0.2.1:1", 204}, {"192.0.2.1:2", 429}, {"[::ffff:192.0.2.1]:3", 429},
		{"192.0.2.2:1", 204}, {"[2001:db8::1]:1", 204}, {"[2001:0db8::1]:2", 429},
		{"[2001:db8::2]:1", 204}, {"invalid", 400}, {"", 400},
	} {
		if w := rateRequest(h, tc.address); w.Code != tc.status {
			t.Fatalf("%s: status=%d, want %d", tc.address, w.Code, tc.status)
		}
	}
	for _, htmx := range []bool{false, true} {
		r := httptest.NewRequest(http.MethodPost, "/?email=other@example.invalid", nil)
		r.RemoteAddr = "192.0.2.1:9999"
		r.Header.Set("X-Forwarded-For", "192.0.2.99")
		r.Header.Set("Forwarded", "for=192.0.2.98")
		r.Header.Set("X-Real-IP", "192.0.2.97")
		if htmx {
			r.Header.Set("HX-Request", "true")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 429 || w.Body.String() != "Too many requests. Please try again later.\n" || w.Header().Get("HX-Redirect") != "" {
			t.Fatal("headers or query changed the rate-limit decision")
		}
	}
	other := testRateLimiter(t, 1, time.Minute, 10).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	if w := rateRequest(other, "192.0.2.1:1"); w.Code != 204 {
		t.Fatal("independent limiters shared state")
	}
}

func TestRateLimiterCapacityAndExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		l := testRateLimiter(t, 2, time.Second, 2)
		h := l.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
		rateRequest(h, "192.0.2.1:1")
		time.Sleep(500 * time.Millisecond)
		rateRequest(h, "192.0.2.2:1")
		for i := 3; i < 100; i++ {
			if w := rateRequest(h, "192.0.2."+strconv.Itoa(i)+":1"); w.Code != 429 || w.Header().Get("Retry-After") != "1" {
				t.Fatal("capacity did not reject a new client")
			}
		}
		if len(l.clients) != 2 || l.expirations.Len() != 2 {
			t.Fatal("client state exceeded capacity")
		}
		if w := rateRequest(h, "192.0.2.1:1"); w.Code != 204 {
			t.Fatal("capacity blocked an existing budget")
		}
		if w := rateRequest(h, "192.0.2.1:1"); w.Code != 429 {
			t.Fatal("new clients evicted active limits")
		}
		time.Sleep(500 * time.Millisecond)
		if w := rateRequest(h, "192.0.2.3:1"); w.Code != 204 {
			t.Fatal("expired entry did not free capacity")
		}
		if len(l.clients) != 2 || l.expirations.Len() != 2 {
			t.Fatal("expiry left stale bookkeeping")
		}
		time.Sleep(time.Second)
		rateRequest(h, "192.0.2.4:1")
		if len(l.clients) != 1 || l.expirations.Len() != 1 {
			t.Fatal("inactive clients were not removed")
		}
	})
}

func TestRateLimiterConcurrentRequests(t *testing.T) {
	l := testRateLimiter(t, 10, time.Hour, 100)
	var allowed atomic.Int64
	h := l.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowed.Add(1)
		w.WriteHeader(204)
	}))
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Go(func() {
			if w := rateRequest(h, "192.0.2.1:1"); w.Code != 204 && w.Code != 429 {
				t.Errorf("unexpected status=%d", w.Code)
			}
		})
	}
	wg.Wait()
	if allowed.Load() != 10 {
		t.Fatalf("allowed=%d, want 10", allowed.Load())
	}
}

func TestRateLimiterConcurrentClientCapacity(t *testing.T) {
	l := testRateLimiter(t, 1, time.Hour, 10)
	var allowed atomic.Int64
	h := l.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowed.Add(1)
		w.WriteHeader(204)
	}))
	var wg sync.WaitGroup
	for i := 1; i <= 100; i++ {
		wg.Go(func() {
			w := rateRequest(h, "192.0.2."+strconv.Itoa(i)+":1")
			if w.Code != 204 && w.Code != 429 {
				t.Errorf("unexpected status=%d", w.Code)
			}
		})
	}
	wg.Wait()
	if allowed.Load() != 10 || len(l.clients) != 10 || l.expirations.Len() != 10 {
		t.Fatalf("allowed=%d clients=%d expirations=%d", allowed.Load(), len(l.clients), l.expirations.Len())
	}
}

type unreadRateLimitedBody struct{ t *testing.T }

func (b unreadRateLimitedBody) Read([]byte) (int, error) {
	b.t.Error("throttled request body was read")
	return 0, io.EOF
}

func TestRateLimiterRejectsBeforeBodyRead(t *testing.T) {
	l := testRateLimiter(t, 1, time.Minute, 1)
	h := l.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(204)
	}))
	rateRequest(h, "192.0.2.1:1")
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.RemoteAddr = "192.0.2.1:1"
	r.Body = io.NopCloser(unreadRateLimitedBody{t})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 429 {
		t.Fatalf("status=%d, want 429", w.Code)
	}
}
