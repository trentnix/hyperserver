package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
)

func testTLSFiles(t *testing.T) (config.HTTPConfig, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "HyperServer test"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:    x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.HTTPConfig{}
	cfg.TLS.Enabled = true
	cfg.TLS.Certificate = filepath.Join(t.TempDir(), "server.pem")
	cfg.TLS.Key = filepath.Join(t.TempDir(), "server.key")
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(cfg.TLS.Certificate, certificate, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.TLS.Key, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), 0600); err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certificate) {
		t.Fatal("invalid test certificate")
	}
	return cfg, roots
}

func TestListenerTLS(t *testing.T) {
	if got, err := listenerTLS(config.HTTPConfig{}); err != nil || got != nil {
		t.Fatalf("disabled TLS: %v, %v", got, err)
	}
	cfg, _ := testTLSFiles(t)
	got, err := listenerTLS(cfg)
	if err != nil || got == nil || got.MinVersion != tls.VersionTLS12 || len(got.Certificates) != 1 {
		t.Fatalf("TLS configuration: %v, %v", got, err)
	}
	other, _ := testTLSFiles(t)
	for _, mode := range []string{"missing certificate", "missing key", "mismatched key", "invalid PEM"} {
		t.Run(mode, func(t *testing.T) {
			invalid := cfg
			switch mode {
			case "missing certificate":
				invalid.TLS.Certificate = filepath.Join(t.TempDir(), "missing")
			case "missing key":
				invalid.TLS.Key = filepath.Join(t.TempDir(), "missing")
			case "mismatched key":
				invalid.TLS.Key = other.TLS.Key
			case "invalid PEM":
				invalid.TLS.Certificate = filepath.Join(t.TempDir(), "bad.pem")
				if err := os.WriteFile(invalid.TLS.Certificate, []byte("not a certificate"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if got, err := listenerTLS(invalid); err == nil || got != nil || !strings.Contains(err.Error(), "http.tls") {
				t.Fatalf("invalid TLS accepted: %v", err)
			}
		})
	}
}

func TestServeHTTPWithTLS(t *testing.T) {
	cfg, roots := testTLSFiles(t)
	tlsConfig, err := listenerTLS(cfg)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{TLSConfig: tlsConfig, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil {
			t.Error("request reached the handler without TLS")
		}
		w.WriteHeader(204)
	})}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serveHTTP(ctx, server, listener) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			server.Close()
			t.Error("TLS server did not shut down")
		}
	})
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	for _, scheme := range []string{"https", "http"} {
		response, err := client.Get(scheme + "://" + listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		want := 204
		if scheme == "http" {
			want = 400
		}
		if response.StatusCode != want {
			t.Fatalf("%s status=%d, want %d", scheme, response.StatusCode, want)
		}
	}

	for _, version := range []uint16{tls.VersionTLS11, tls.VersionTLS12, tls.VersionTLS13} {
		t.Run(tls.VersionName(version), func(t *testing.T) {
			transport := &http.Transport{TLSClientConfig: &tls.Config{
				RootCAs: roots, MinVersion: version, MaxVersion: version,
			}}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
			response, err := client.Get("https://" + listener.Addr().String())
			if err != nil {
				if version < tls.VersionTLS12 {
					return
				}
				t.Fatal(err)
			}
			defer response.Body.Close()
			if version < tls.VersionTLS12 {
				t.Fatal("listener accepted TLS below 1.2")
			}
			if response.StatusCode != 204 || response.TLS.Version != version {
				t.Fatalf("status=%d TLS=%s", response.StatusCode, tls.VersionName(response.TLS.Version))
			}
		})
	}
}
