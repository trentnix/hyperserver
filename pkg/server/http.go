package server

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/trentnix/hyperserver/config"
)

// NewHTTPServer builds a standard HTTP server with the configured address,
// timeouts, and optional TLS certificate. It does not open a listener, wrap the
// handler, or start serving. The handler must not be nil.
// Resolve configuration paths before calling. Timeout values are passed through
// unchanged, including zero. An empty listen host defaults to 127.0.0.1.
// Use ServeHTTP or ApplicationServer.Run to serve with graceful shutdown.
func NewHTTPServer(cfg config.HTTPConfig, handler http.Handler) (*http.Server, error) {
	if handler == nil {
		return nil, fmt.Errorf("HTTP server requires a handler")
	}
	tlsConfig, err := listenerTLS(cfg)
	if err != nil {
		return nil, err
	}
	host := strings.TrimSpace(cfg.ListenHost)
	if host == "" {
		host = "127.0.0.1"
	}
	return &http.Server{
		Addr:         net.JoinHostPort(host, strconv.Itoa(int(cfg.Port))),
		Handler:      handler,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
		TLSConfig:    tlsConfig,
	}, nil
}

// listenerTLS loads the configured key pair before opening the listening socket.
func listenerTLS(cfg config.HTTPConfig) (*tls.Config, error) {
	if !cfg.TLS.Enabled {
		if cfg.TLS.Certificate != "" || cfg.TLS.Key != "" {
			return nil, fmt.Errorf("http.tls.enabled must be true when TLS certificate or key files are configured")
		}
		return nil, nil
	}
	certificate, err := tls.LoadX509KeyPair(cfg.TLS.Certificate, cfg.TLS.Key)
	if err != nil {
		return nil, fmt.Errorf("http.tls: load certificate and key: %w", err)
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}, nil
}
