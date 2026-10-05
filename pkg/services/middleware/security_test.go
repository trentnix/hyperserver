package middleware

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/trentnix/hyperserver/pkg/requestinfo"
)

func TestSecurityHeaders(t *testing.T) {
	for _, policy := range []string{"", "default-src 'none'; frame-ancestors 'none'"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)
		SecurityHeaders(policy)(http.HandlerFunc(func(received http.ResponseWriter, request *http.Request) {
			if received != w || request != r {
				t.Error("middleware replaced the writer or request")
			}
			http.Error(received, "Rejected", 403)
		})).ServeHTTP(w, r)
		for name, want := range map[string]string{
			"Content-Security-Policy": policy,
			"X-Content-Type-Options":  "nosniff",
			"X-Frame-Options":         "DENY",
			"Referrer-Policy":         "no-referrer",
			"Permissions-Policy":      "camera=(), microphone=(), geolocation=()",
		} {
			if got := w.Result().Header.Get(name); got != want {
				t.Errorf("%s=%q, want %q", name, got, want)
			}
		}
		if w.Code != 403 || w.Body.String() != "Rejected\n" {
			t.Fatal("middleware changed the response")
		}
	}
}

func TestHSTSRequiresVerifiedHTTPS(t *testing.T) {
	if handler, err := NewHSTS(-1); err == nil || handler != nil {
		t.Fatal("negative max age accepted")
	}
	proxy, err := requestinfo.NewProxyMiddleware([]string{"192.0.2.1/32"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, peer, scheme, want string
		tls                      bool
		maxAge                   int64
	}{
		{"HTTP", "192.0.2.2:1", "", "", false, 86400},
		{"direct HTTPS", "192.0.2.2:1", "", "max-age=86400", true, 86400},
		{"spoofed HTTPS", "192.0.2.2:1", "https", "", false, 86400},
		{"trusted proxy HTTPS", "192.0.2.1:1", "https", "max-age=86400", false, 86400},
		{"trusted proxy HTTP over TLS", "192.0.2.1:1", "http", "", true, 86400},
		{"disabled", "192.0.2.2:1", "", "", true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hsts, err := NewHSTS(tc.maxAge)
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tc.peer
			r.Header.Set("X-Forwarded-For", "203.0.113.1")
			r.Header.Set("X-Forwarded-Proto", tc.scheme)
			if tc.tls {
				r.TLS = &tls.ConnectionState{}
			}
			w := httptest.NewRecorder()
			proxy(hsts(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(204)
			}))).ServeHTTP(w, r)
			if w.Code != 204 || w.Result().Header.Get("Strict-Transport-Security") != tc.want {
				t.Fatalf("status=%d headers=%v", w.Code, w.Header())
			}
		})
	}
}
