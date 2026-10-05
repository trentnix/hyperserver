package requestinfo

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProxyRequestInformation(t *testing.T) {
	for _, tc := range []struct {
		name, peer, client, scheme, wantIP string
		trusted                            []string
		backendTLS, wantHTTPS, invalid     bool
	}{
		{name: "direct HTTP ignores spoofing", peer: "192.0.2.1:1", client: "203.0.113.1", scheme: "https", wantIP: "192.0.2.1"},
		{name: "direct TLS ignores downgrade", peer: "192.0.2.1:1", client: "203.0.113.1", scheme: "http", backendTLS: true, wantHTTPS: true, wantIP: "192.0.2.1"},
		{name: "trusted HTTPS", peer: "192.0.2.1:1", client: "203.0.113.1", scheme: "https", trusted: []string{"192.0.2.1/32"}, wantHTTPS: true, wantIP: "203.0.113.1"},
		{name: "trusted HTTP over TLS backend", peer: "192.0.2.1:1", client: "203.0.113.1", scheme: "http", backendTLS: true, trusted: []string{"192.0.2.1/32"}, wantIP: "203.0.113.1"},
		{name: "outside trusted range", peer: "192.0.3.1:1", client: "203.0.113.1", scheme: "https", trusted: []string{"192.0.2.0/24"}, wantIP: "192.0.3.1"},
		{name: "adjacent to trusted host", peer: "192.0.2.2:1", client: "203.0.113.1", scheme: "https", trusted: []string{"192.0.2.1/32"}, wantIP: "192.0.2.2"},
		{name: "mapped IPv4", peer: "[::ffff:192.0.2.1]:1", client: "::ffff:203.0.113.1", scheme: "https", trusted: []string{"192.0.2.1/32"}, wantHTTPS: true, wantIP: "203.0.113.1"},
		{name: "IPv6", peer: "[2001:db8::1]:1", client: "2001:db8:1::2", scheme: "https", trusted: []string{"2001:db8::/64"}, wantHTTPS: true, wantIP: "2001:db8:1::2"},
		{name: "multiple trusted ranges", peer: "192.0.2.1:1", client: "203.0.113.1", scheme: "https", trusted: []string{"2001:db8::/64", "192.0.2.0/24"}, wantHTTPS: true, wantIP: "203.0.113.1"},
		{name: "missing headers", peer: "192.0.2.1:1", trusted: []string{"192.0.2.1/32"}, invalid: true},
		{name: "missing scheme", peer: "192.0.2.1:1", client: "203.0.113.1", trusted: []string{"192.0.2.1/32"}, invalid: true},
		{name: "missing client", peer: "192.0.2.1:1", scheme: "https", trusted: []string{"192.0.2.1/32"}, invalid: true},
		{name: "client chain", peer: "192.0.2.1:1", client: "203.0.113.1, 203.0.113.2", scheme: "https", trusted: []string{"192.0.2.1/32"}, invalid: true},
		{name: "scheme chain", peer: "192.0.2.1:1", client: "203.0.113.1", scheme: "https, http", trusted: []string{"192.0.2.1/32"}, invalid: true},
		{name: "invalid scheme", peer: "192.0.2.1:1", client: "203.0.113.1", scheme: "ftp", trusted: []string{"192.0.2.1/32"}, invalid: true},
		{name: "client includes port", peer: "192.0.2.1:1", client: "203.0.113.1:443", scheme: "https", trusted: []string{"192.0.2.1/32"}, invalid: true},
		{name: "client includes zone", peer: "192.0.2.1:1", client: "fe80::1%eth0", scheme: "https", trusted: []string{"192.0.2.1/32"}, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proxy, err := NewProxyMiddleware(tc.trusted)
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("GET", "http://example.test/path", nil)
			r.RemoteAddr = tc.peer
			if tc.backendTLS {
				r.TLS = &tls.ConnectionState{}
			}
			if tc.client != "" {
				r.Header.Set("X-Forwarded-For", tc.client)
			}
			if tc.scheme != "" {
				r.Header.Set("X-Forwarded-Proto", tc.scheme)
			}
			r.Header.Set("Forwarded", "for=198.51.100.99;proto=https;host=attacker.test")
			r.Header.Set("X-Forwarded-Host", "attacker.test")
			called := false
			handler := proxy(http.HandlerFunc(func(w http.ResponseWriter, received *http.Request) {
				called = true
				ip, err := ClientIP(received)
				if err != nil || ip.String() != tc.wantIP || IsHTTPS(received) != tc.wantHTTPS {
					t.Errorf("client=%v HTTPS=%t error=%v", ip, IsHTTPS(received), err)
				}
				if received.RemoteAddr != r.RemoteAddr || received.TLS != r.TLS || received.Host != r.Host || received.URL != r.URL {
					t.Error("proxy metadata replaced net/http connection or URL fields")
				}
				w.WriteHeader(204)
			}))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if tc.invalid {
				if called || w.Code != 400 || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("invalid metadata: called=%t status=%d", called, w.Code)
				}
			} else if !called || w.Code != 204 {
				t.Fatalf("called=%t status=%d", called, w.Code)
			}
			if IsHTTPS(r) != tc.backendTLS {
				t.Error("middleware modified the caller's request context")
			}
		})
	}
}

func TestProxyConcurrentRequestsAndConfigurationSnapshot(t *testing.T) {
	trusted := []string{"192.0.2.1/32"}
	proxy, err := NewProxyMiddleware(trusted)
	if err != nil {
		t.Fatal(err)
	}
	// Later changes to the caller's configuration must not change existing middleware.
	trusted[0] = "198.51.100.1/32"
	type expectation struct {
		client string
		https  bool
	}
	type expectationKey struct{}
	handler := proxy(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := r.Context().Value(expectationKey{}).(expectation)
		client, err := ClientIP(r)
		if err != nil || client.String() != want.client || IsHTTPS(r) != want.https || r.Context().Err() != context.Canceled {
			t.Errorf("client=%v HTTPS=%t error=%v context=%v, want %+v", client, IsHTTPS(r), err, r.Context().Err(), want)
		}
		w.WriteHeader(204)
	}))
	for i := range 32 {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			client := fmt.Sprintf("203.0.113.%d", i+1)
			secure := i%2 == 0
			want := expectation{client: client, https: secure}
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = "192.0.2.1:1234"
			if i%3 == 0 {
				r.RemoteAddr = "198.51.100.1:1234"
				want = expectation{client: "198.51.100.1"}
			}
			r.Header.Set("X-Forwarded-For", client)
			scheme := "http"
			if secure {
				scheme = "https"
			}
			r.Header.Set("X-Forwarded-Proto", scheme)
			ctx, cancel := context.WithCancel(context.WithValue(r.Context(), expectationKey{}, want))
			cancel()
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r.WithContext(ctx))
			if w.Code != 204 {
				t.Fatalf("status=%d", w.Code)
			}
		})
	}
}

func TestProxyRejectsDuplicateHeaders(t *testing.T) {
	proxy, err := NewProxyMiddleware([]string{"192.0.2.1/32"})
	if err != nil {
		t.Fatal(err)
	}
	for _, header := range []string{"X-Forwarded-For", "X-Forwarded-Proto"} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = "192.0.2.1:1"
		r.Header.Set("X-Forwarded-For", "203.0.113.1")
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Add(header, r.Header.Get(header))
		w := httptest.NewRecorder()
		proxy(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("ambiguous request admitted") })).ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("%s status=%d", header, w.Code)
		}
	}
}

func TestProxyConfigurationAndDirectAddressErrors(t *testing.T) {
	for _, cidr := range []string{"", "localhost", "127.0.0.1", "192.0.2.1/33", "::ffff:192.0.2.1/128"} {
		if middleware, err := NewProxyMiddleware([]string{cidr}); err == nil || middleware != nil {
			t.Fatalf("accepted %q", cidr)
		}
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "invalid"
	if _, err := ClientIP(r); err == nil {
		t.Fatal("invalid direct peer accepted")
	}
}
