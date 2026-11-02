package middleware

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/requestinfo"
)

func TestConfiguredFormActionOrigins(t *testing.T) {
	for _, tc := range []struct {
		policy  string
		want    string
		invalid bool
	}{
		{"default-src 'none'; form-action 'self'; frame-ancestors 'none'", "form-action 'self' https://login.example", false},
		{"default-src 'none'", "form-action 'self' https://login.example", false},
		{"form-action 'self' https://login.example", "form-action 'self' https://login.example", false},
		{"", "", true},
		{"form-action 'none'", "", true},
		{"form-action", "", true},
		{"form-action 'self'; FORM-ACTION 'self'", "", true},
	} {
		t.Run(tc.policy, func(t *testing.T) {
			handler, err := Web(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }), config.HTTPConfig{FormActionOrigins: []string{"https://login.example"}}, tc.policy, nil)
			if tc.invalid {
				if err == nil {
					t.Fatal("accepted contradictory or absent CSP")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
			policy := w.Header().Get("Content-Security-Policy")
			if !strings.Contains(policy, tc.want) || strings.Count(policy, "https://login.example") != 1 {
				t.Fatal("incorrect policy", policy)
			}
			// Allowing an outgoing form destination must not authorize inbound CSRF.
			r := httptest.NewRequest("POST", "https://site.example/", nil)
			r.Header.Set("Origin", "https://login.example")
			r.Header.Set("Sec-Fetch-Site", "cross-site")
			w = httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != http.StatusForbidden {
				t.Fatalf("CSRF status=%d", w.Code)
			}
		})
	}
}

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
