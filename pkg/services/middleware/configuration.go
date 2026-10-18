package middleware

import (
	"fmt"
	"net/http"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/ratelimit"
	"github.com/trentnix/hyperserver/pkg/requestinfo"
)

// FromConfig wraps handler with proxy verification, HSTS, and the optional shared
// per-client rate limit, in that request order. Each call owns independent counters.
// Place session loading and origin checks inside this wrapper. Place request
// logging and SecurityHeaders outside it so they also cover rejected requests.
// It does not configure route defaults, authentication, sessions, CSRF, or CSP.
func FromConfig(handler http.Handler, cfg config.HTTPConfig) (http.Handler, error) {
	if handler == nil {
		return nil, fmt.Errorf("HTTP middleware requires a handler")
	}
	proxy, err := requestinfo.NewProxyMiddleware(cfg.TrustedProxies)
	if err != nil {
		return nil, fmt.Errorf("http.trustedProxies: %w", err)
	}
	hsts, err := NewHSTS(cfg.HSTSMaxAge)
	if err != nil {
		return nil, fmt.Errorf("http.hstsMaxAge: %w", err)
	}

	if limit := cfg.SharedRateLimit; limit.Enabled {
		limiter, err := ratelimit.New("shared application", ratelimit.Policy{Requests: limit.Requests, Window: limit.Window, MaxClients: limit.MaxClients})
		if err != nil {
			return nil, fmt.Errorf("http.sharedRateLimit: %w", err)
		}
		handler = limiter.Handler(handler)
	}
	return proxy(hsts(handler)), nil
}
