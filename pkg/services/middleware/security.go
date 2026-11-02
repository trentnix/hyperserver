package middleware

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/trentnix/hyperserver/pkg/requestinfo"
)

// withFormActionOrigins extends only form-action. Origins are validated by
// HTTPConfig. Refuse contradictory or absent policies instead of silently
// weakening an application's explicit denial or ignoring its configuration.
func withFormActionOrigins(policy string, origins []string) (string, error) {
	if len(origins) == 0 {
		return policy, nil
	}
	if strings.TrimSpace(policy) == "" {
		return "", fmt.Errorf("http.formActionOrigins requires an application Content Security Policy")
	}
	directives := strings.Split(policy, ";")
	found := false
	for i, directive := range directives {
		fields := strings.Fields(directive)
		if len(fields) == 0 || !strings.EqualFold(fields[0], "form-action") {
			continue
		}
		if found || len(fields) == 1 || slices.Contains(fields[1:], "'none'") {
			return "", fmt.Errorf("http.formActionOrigins requires a single nonempty form-action directive that does not contain 'none'")
		}
		found = true
		for _, origin := range origins {
			if !slices.Contains(fields[1:], origin) {
				fields = append(fields, origin)
			}
		}
		directives[i] = " " + strings.Join(fields, " ")
	}
	if !found {
		directives = append(directives, " form-action 'self' "+strings.Join(origins, " "))
	}
	return strings.TrimSpace(strings.Join(directives, ";")), nil
}

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
