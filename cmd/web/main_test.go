package main

import "testing"

func TestReferenceListenAddress(t *testing.T) {
	tests := []struct {
		name     string
		hostname string
		port     uint16
		want     string
	}{
		{name: "empty hostname", port: 8080, want: "127.0.0.1:8080"},
		{name: "blank hostname", hostname: " \t", port: 8080, want: "127.0.0.1:8080"},
		{name: "localhost", hostname: "localhost", port: 8080, want: "127.0.0.1:8080"},
		{name: "uppercase localhost", hostname: "LOCALHOST", port: 8080, want: "127.0.0.1:8080"},
		{name: "IPv4 loopback", hostname: "127.0.0.1", port: 8080, want: "127.0.0.1:8080"},
		{name: "IPv4 loopback range", hostname: "127.0.0.2", port: 8080, want: "127.0.0.2:8080"},
		{name: "IPv6 loopback", hostname: "::1", port: 8080, want: "[::1]:8080"},
		{name: "expanded IPv6 loopback", hostname: "0:0:0:0:0:0:0:1", port: 8080, want: "[::1]:8080"},
		{name: "IPv4-mapped loopback", hostname: "::ffff:127.0.0.1", port: 8080, want: "127.0.0.1:8080"},
		{name: "surrounding whitespace", hostname: " 127.0.0.1 ", port: 8080, want: "127.0.0.1:8080"},
		{name: "ephemeral port", hostname: "127.0.0.1", port: 0, want: "127.0.0.1:0"},
		{name: "maximum port", hostname: "127.0.0.1", port: 65535, want: "127.0.0.1:65535"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := referenceListenAddress(tt.hostname, tt.port)
			if err != nil {
				t.Fatalf("referenceListenAddress() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("referenceListenAddress() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReferenceListenAddressRejectsNonLoopback(t *testing.T) {
	for _, hostname := range []string{
		"0.0.0.0",
		"::",
		"::ffff:0.0.0.0",
		"192.168.1.10",
		"10.0.0.1",
		"203.0.113.1",
		"2001:db8::1",
		"::ffff:192.168.1.10",
		"example.com",
		"localhost.example.com",
		"localhost.",
		"*",
		"127.0.0.1:8080",
		"[::1]",
	} {
		t.Run(hostname, func(t *testing.T) {
			address, err := referenceListenAddress(hostname, 8080)
			if err == nil {
				t.Fatal("referenceListenAddress() must reject a non-loopback or invalid hostname")
			}
			if address != "" {
				t.Errorf("referenceListenAddress() returned address %q with an error", address)
			}
		})
	}
}
