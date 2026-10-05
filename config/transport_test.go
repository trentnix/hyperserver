package config

import (
	"slices"
	"strings"
	"testing"
)

func TestTransportConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, yaml, wantErr string
		env                 map[string]string
		proxies             []string
		tls                 bool
	}{
		{name: "defaults", yaml: "{}"},
		{name: "explicitly no proxies", yaml: "http:\n  trustedProxies: []"},
		{name: "direct TLS", yaml: "http:\n  tls:\n    enabled: true\n    certificate: server.pem\n    key: server.key", tls: true},
		{name: "missing key", yaml: "http:\n  tls:\n    enabled: true\n    certificate: server.pem", wantErr: "http.tls.key"},
		{name: "missing certificate", yaml: "http:\n  tls:\n    enabled: true\n    key: server.key", wantErr: "http.tls.certificate"},
		{name: "unused files", yaml: "http:\n  tls:\n    certificate: server.pem\n    key: server.key", wantErr: "http.tls.enabled"},
		{name: "trusted proxy", yaml: "http:\n  publicOrigin: https://example.test\n  trustedProxies: [127.0.0.1/32, '::1/128']", proxies: []string{"127.0.0.1/32", "::1/128"}},
		{name: "invalid proxy", yaml: "http:\n  trustedProxies: [localhost]", wantErr: "http.trustedProxies"},
		{name: "proxy needs origin", yaml: "http:\n  trustedProxies: [127.0.0.1/32]", wantErr: "http.publicOrigin"},
		{name: "environment", yaml: "{}", env: map[string]string{"HYPERSERVER_HTTP_PUBLICORIGIN": "https://example.test", "HYPERSERVER_HTTP_TRUSTEDPROXIES": "127.0.0.1/32,::1/128"}, proxies: []string{"127.0.0.1/32", "::1/128"}},
		{name: "environment replaces list", yaml: "http:\n  publicOrigin: https://example.test\n  trustedProxies: [192.0.2.1/32]", env: map[string]string{"HYPERSERVER_HTTP_TRUSTEDPROXIES": "127.0.0.1/32"}, proxies: []string{"127.0.0.1/32"}},
		{name: "environment clears list", yaml: "http:\n  trustedProxies: [192.0.2.1/32]", env: map[string]string{"HYPERSERVER_HTTP_TRUSTEDPROXIES": ""}},
		{name: "invalid environment", yaml: "{}", env: map[string]string{"HYPERSERVER_HTTP_TRUSTEDPROXIES": "invalid"}, wantErr: "http.trustedProxies"},
		{name: "TLS environment", yaml: "{}", env: map[string]string{"HYPERSERVER_HTTP_TLS_ENABLED": "true", "HYPERSERVER_HTTP_TLS_CERTIFICATE": "server.pem", "HYPERSERVER_HTTP_TLS_KEY": "server.key"}, tls: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cleanConfigEnvironment(t)
			for key, value := range tc.env {
				t.Setenv(key, value)
			}
			cfg, err := loadTestConfig(t, tc.yaml)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error=%v, want %s", err, tc.wantErr)
				}
				return
			}
			if err != nil || cfg.HTTP.TLS.Enabled != tc.tls || !slices.Equal(cfg.HTTP.TrustedProxies, tc.proxies) {
				t.Fatalf("TLS=%t proxies=%v error=%v", cfg.HTTP.TLS.Enabled, cfg.HTTP.TrustedProxies, err)
			}
			if tc.tls && (cfg.HTTP.TLS.Certificate != "server.pem" || cfg.HTTP.TLS.Key != "server.key") {
				t.Fatalf("certificate=%q key=%q", cfg.HTTP.TLS.Certificate, cfg.HTTP.TLS.Key)
			}
		})
	}
}
