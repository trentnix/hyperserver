package util

import (
	"crypto/tls"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/config"
)

func TestBuildPublicURLUsesConfiguredOrigin(t *testing.T) {
	cfg := config.HTTPConfig{ListenHost: "127.0.0.1", Port: 8080, PublicOrigin: "https://example.com/"}
	r := httptest.NewRequest("GET", "http://attacker.example/", nil)
	r.Header.Set("X-Forwarded-Proto", "https://attacker.example/?leak=")
	for _, path := range []string{"/auth/verify", "/auth/reset/email", "/callbacks/complete"} {
		link, err := BuildPublicURL(r, cfg, path, map[string]string{"token": "a+b&c"})
		if err != nil || link.String() != "https://example.com"+path+"?token=a%2Bb%26c" {
			t.Fatalf("link = %v, error = %v", link, err)
		}
	}
	if cfg.PublicOrigin != "https://example.com/" {
		t.Fatal("building a link changed the configuration")
	}
}

func TestBuildPublicURLRejectsInvalidOrigin(t *testing.T) {
	cfg := config.HTTPConfig{ListenHost: "localhost", Port: 8080, PublicOrigin: "https://example.com/path"}
	if _, err := BuildPublicURL(httptest.NewRequest("GET", "/", nil), cfg, "/reset", nil); err == nil {
		t.Fatal("invalid public origin fell back to listener settings")
	}
}

func TestBuildPublicURLWithoutRequest(t *testing.T) {
	cfg := config.HTTPConfig{PublicOrigin: "https://example.com"}
	link, err := BuildPublicURL(nil, cfg, "/shared", nil)
	if err != nil || link.String() != "https://example.com/shared" {
		t.Fatalf("link = %v, error = %v", link, err)
	}
}

func TestBuildPublicURLFallbackIgnoresForwardedHeaders(t *testing.T) {
	for _, header := range []string{"https", "http", "https://attacker.example/?leak=", "https,http"} {
		for _, secure := range []bool{false, true} {
			r := httptest.NewRequest("GET", "http://untrusted.example/", nil)
			r.Header.Set("X-Forwarded-Proto", header)
			r.Header.Set("X-Forwarded-Host", "attacker.example")
			wantScheme := "http"
			if secure {
				r.TLS = &tls.ConnectionState{}
				wantScheme = "https"
			}
			cfg := config.HTTPConfig{ListenHost: "trusted.example", Port: 8080}
			link, err := BuildPublicURL(r, cfg, "/reset", map[string]string{"token": "a+b&c"})
			if err != nil {
				t.Fatal(err)
			}
			if link.String() != wantScheme+"://trusted.example:8080/reset?token=a%2Bb%26c" {
				t.Errorf("header %q: link = %s", header, link)
			}
		}
	}
}

func TestBuildPublicURLFallbackAddresses(t *testing.T) {
	for _, tc := range []struct {
		target, host string
		port         uint16
		want         string
	}{
		{"http://localhost/", "localhost", 80, "http://localhost/reset"},
		{"https://localhost/", "localhost", 443, "https://localhost/reset"},
		{"http://localhost/", "::1", 80, "http://[::1]/reset"},
		{"https://localhost/", "::1", 443, "https://[::1]/reset"},
		{"https://localhost/", "::1", 8443, "https://[::1]:8443/reset"},
		{"http://localhost/", "localhost", 8080, "http://localhost:8080/reset"},
		{"http://localhost/", "", 8080, "http://127.0.0.1:8080/reset"},
		{"http://localhost/", " \t", 8080, "http://127.0.0.1:8080/reset"},
		{"http://localhost/", " 127.0.0.1 ", 8080, "http://127.0.0.1:8080/reset"},
		{"http://localhost/", "localhost", 443, "http://localhost:443/reset"},
		{"https://localhost/", "localhost", 80, "https://localhost:80/reset"},
	} {
		cfg := config.HTTPConfig{ListenHost: tc.host, Port: tc.port}
		link, err := BuildPublicURL(httptest.NewRequest("GET", tc.target, nil), cfg, "/reset", nil)
		if err != nil || link.String() != tc.want {
			t.Errorf("link = %v, err = %v, want %s", link, err, tc.want)
		}
	}
}

func TestBuildPublicURLFallbackRequiresRequest(t *testing.T) {
	cfg := config.HTTPConfig{ListenHost: "localhost", Port: 80}
	if _, err := BuildPublicURL(nil, cfg, "/reset", nil); err == nil {
		t.Error("accepted a missing request")
	}
}

func TestBuildPublicURLFallbackRequiresPort(t *testing.T) {
	cfg := config.HTTPConfig{ListenHost: "localhost"}
	// A port-zero listener is valid, but it cannot supply an absolute link's port.
	if err := (config.Config{HTTP: cfg}).Validate(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "http://localhost:8080/", nil)
	if link, err := BuildPublicURL(r, cfg, "/reset", nil); err == nil || link != nil {
		t.Fatalf("missing port: link = %v, error = %v", link, err)
	}
	cfg.PublicOrigin = "https://example.com"
	link, err := BuildPublicURL(r, cfg, "/reset", nil)
	if err != nil || link.String() != "https://example.com/reset" {
		t.Fatalf("configured origin with port-zero listener: link = %v, error = %v", link, err)
	}
}

func TestBuildPublicURLWildcardListenerRequiresPublicOrigin(t *testing.T) {
	for _, host := range []string{"0.0.0.0", "::", "::ffff:0.0.0.0"} {
		t.Run(host, func(t *testing.T) {
			cfg := config.HTTPConfig{ListenHost: host, Port: 8080}
			if err := (config.Config{HTTP: cfg}).Validate(); err != nil {
				t.Fatalf("wildcard listeners must remain valid framework configuration: %v", err)
			}
			r := httptest.NewRequest("GET", "http://untrusted.example/", nil)
			if link, err := BuildPublicURL(r, cfg, "/reset", nil); err == nil || link != nil || !strings.Contains(err.Error(), "http.publicOrigin") {
				t.Fatalf("wildcard fallback: link = %v, error = %v", link, err)
			}
			cfg.PublicOrigin = "https://example.com"
			link, err := BuildPublicURL(r, cfg, "/reset", nil)
			if err != nil || link.String() != "https://example.com/reset" {
				t.Fatalf("configured origin: link = %v, error = %v", link, err)
			}
		})
	}
}
