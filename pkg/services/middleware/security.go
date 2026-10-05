package middleware

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/trentnix/hyperserver/pkg/requestinfo"
)

// SecurityHeaders sets browser protections before downstream handlers run.
// The application supplies a trusted CSP matching its assets and HTMX settings.
// An empty policy omits CSP. Install outside middleware that can reject requests.
func SecurityHeaders(contentSecurityPolicy string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
			if contentSecurityPolicy != "" {
				w.Header().Set("Content-Security-Policy", contentSecurityPolicy)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// NewHSTS emits Strict-Transport-Security only for verified client-facing HTTPS.
// maxAgeSeconds must be nonnegative. Zero disables the header, not a browser's
// remembered policy. Subdomains and preload are deliberately not included.
// Install after proxy verification, before middleware that can reject requests.
func NewHSTS(maxAgeSeconds int64) (func(http.Handler) http.Handler, error) {
	if maxAgeSeconds < 0 {
		return nil, fmt.Errorf("HSTS max age must not be negative")
	}
	value := "max-age=" + strconv.FormatInt(maxAgeSeconds, 10)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if maxAgeSeconds > 0 && requestinfo.IsHTTPS(r) {
				w.Header().Set("Strict-Transport-Security", value)
			}
			next.ServeHTTP(w, r)
		})
	}, nil
}
