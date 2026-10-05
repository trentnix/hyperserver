// Package requestinfo reports client addresses and HTTPS state without replacing
// net/http's connection metadata. Forwarding headers require an explicitly trusted proxy.
package requestinfo

import (
	"context"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
)

type contextKey struct{}

type forwardedRequest struct {
	client netip.Addr
	https  bool
}

// ClientIP returns the verified forwarded address, or the direct peer address.
// IPv4-mapped addresses use their IPv4 representation. Source ports are ignored.
func ClientIP(r *http.Request) (netip.Addr, error) {
	if info, ok := r.Context().Value(contextKey{}).(forwardedRequest); ok {
		return info.client, nil
	}
	peer, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}, err
	}
	return peer.Addr().Unmap().WithZone(""), nil
}

// IsHTTPS reports the client-facing scheme. Without verified proxy metadata,
// only the actual TLS connection establishes HTTPS. Headers alone never do.
func IsHTTPS(r *http.Request) bool {
	if info, ok := r.Context().Value(contextKey{}).(forwardedRequest); ok {
		return info.https
	}
	return r.TLS != nil
}

// NewProxyMiddleware trusts only direct peers within the supplied CIDR ranges.
// Configure one edge proxy to overwrite X-Forwarded-For with one client IP and
// X-Forwarded-Proto with http or https. Missing, duplicate, and chained values
// from trusted peers are rejected with 400. Headers from other peers are ignored.
// Forwarded and X-Forwarded-Host are not used. Preserve the browser-facing Host
// at the proxy for origin checks. Install before rate limits and session loading.
// RemoteAddr, Host, URL, and TLS retain their net/http meanings.
func NewProxyMiddleware(trustedCIDRs []string) (func(http.Handler) http.Handler, error) {
	trusted := make([]netip.Prefix, 0, len(trustedCIDRs))
	for _, value := range trustedCIDRs {
		prefix, err := netip.ParsePrefix(value)
		if err != nil || prefix.Addr().Is4In6() {
			return nil, fmt.Errorf("invalid trusted proxy CIDR %q: use an IPv4 or IPv6 network", value)
		}
		trusted = append(trusted, prefix.Masked())
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Trust is based on the transport peer, never a supplied header or context.
			peer, err := netip.ParseAddrPort(r.RemoteAddr)
			isTrusted := false
			if err == nil {
				for _, prefix := range trusted {
					if prefix.Contains(peer.Addr().Unmap().WithZone("")) {
						isTrusted = true
						break
					}
				}
			}
			if !isTrusted {
				next.ServeHTTP(w, r)
				return
			}

			clients, schemes := r.Header.Values("X-Forwarded-For"), r.Header.Values("X-Forwarded-Proto")
			if len(clients) != 1 || len(schemes) != 1 {
				rejectForwarding(w)
				return
			}
			client, err := netip.ParseAddr(strings.TrimSpace(clients[0]))
			scheme := strings.TrimSpace(schemes[0])
			if err != nil || client.Zone() != "" || (scheme != "http" && scheme != "https") {
				rejectForwarding(w)
				return
			}
			info := forwardedRequest{client: client.Unmap(), https: scheme == "https"}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), contextKey{}, info)))
		})
	}, nil
}

func rejectForwarding(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, "Invalid proxy forwarding headers", http.StatusBadRequest)
}
