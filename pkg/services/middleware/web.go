package middleware

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/trentnix/hyperserver/config"
)

// Web wraps an application's handler with logging, browser security headers,
// proxy verification, HSTS, the shared rate limit, and cross-origin protection,
// in that request order. Supply session and identity loading inside handler.
// The application chooses its CSP. Empty omits CSP. A nil log disables logging.
// Route defaults belong to handlers.Setup. Use the individual middleware when
// an application needs a different stack or cross-origin policy.
func Web(handler http.Handler, cfg config.HTTPConfig, contentSecurityPolicy string, log *slog.Logger) (http.Handler, error) {
	if handler == nil {
		return nil, errors.New("HTTP middleware requires a handler")
	}
	handler = http.NewCrossOriginProtection().Handler(handler)
	handler, err := FromConfig(handler, cfg)
	if err != nil {
		return nil, err
	}
	return LoggerMiddleware(log)(SecurityHeaders(contentSecurityPolicy)(handler)), nil
}
