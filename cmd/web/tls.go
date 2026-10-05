package main

import (
	"crypto/tls"
	"fmt"

	"github.com/trentnix/hyperserver/config"
)

// listenerTLS loads the configured key pair before opening the listening socket.
func listenerTLS(cfg config.HTTPConfig) (*tls.Config, error) {
	if !cfg.TLS.Enabled {
		return nil, nil
	}
	certificate, err := tls.LoadX509KeyPair(cfg.TLS.Certificate, cfg.TLS.Key)
	if err != nil {
		return nil, fmt.Errorf("http.tls: load certificate and key: %w", err)
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}, nil
}
