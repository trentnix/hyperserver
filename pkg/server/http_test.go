package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
)

func TestNewHTTPServer(t *testing.T) {
	for _, test := range []struct{ host, address string }{
		{"", "127.0.0.1:8080"},
		{"0.0.0.0", "0.0.0.0:8080"},
		{"::", "[::]:8080"},
		{"localhost", "localhost:8080"},
	} {
		t.Run(test.address, func(t *testing.T) {
			handler := http.NewServeMux()
			cfg := config.HTTPConfig{ListenHost: test.host, Port: 8080, ReadTimeout: 3 * time.Second,
				WriteTimeout: 5 * time.Second, IdleTimeout: time.Minute}
			got, err := NewHTTPServer(cfg, handler)
			if err != nil {
				t.Fatal(err)
			}
			if got.Addr != test.address || got.Handler != handler || got.TLSConfig != nil ||
				got.ReadTimeout != cfg.ReadTimeout || got.WriteTimeout != cfg.WriteTimeout || got.IdleTimeout != cfg.IdleTimeout {
				t.Fatalf("server configuration: %+v", got)
			}
		})
	}
}

func TestNewHTTPServerZeroConfiguration(t *testing.T) {
	called := false
	got, err := NewHTTPServer(config.HTTPConfig{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got.Addr != "127.0.0.1:0" || got.ReadTimeout != 0 || got.WriteTimeout != 0 || got.IdleTimeout != 0 || called {
		t.Fatalf("zero configuration changed or handler invoked: %+v", got)
	}
	w := httptest.NewRecorder()
	got.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if !called || w.Code != http.StatusNoContent || len(w.Header()) != 0 {
		t.Fatal("constructor wrapped or replaced supplied handler")
	}
}

func TestNewHTTPServerRejectsInvalidSetup(t *testing.T) {
	for _, mode := range []string{"nil handler", "missing TLS files", "disabled TLS files"} {
		t.Run(mode, func(t *testing.T) {
			cfg := config.HTTPConfig{}
			var handler http.Handler = http.NewServeMux()
			switch mode {
			case "nil handler":
				handler = nil
			case "missing TLS files":
				cfg.TLS.Enabled = true
			case "disabled TLS files":
				cfg.TLS.Certificate = "unused.pem"
			}
			got, err := NewHTTPServer(cfg, handler)
			if got != nil || err == nil {
				t.Fatal("invalid setup accepted")
			}
			if mode != "nil handler" && !strings.Contains(err.Error(), "http.tls") {
				t.Fatal("TLS error lost setting name:", err)
			}
		})
	}
}
