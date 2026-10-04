package util

import (
	"net/http"
	"net/url"
	"strings"
)

// SafeRedirectURL selects a local destination, then fallback, then "/".
// Destinations must start with a single slash. Absolute URLs (even same-host
// URLs), network-path references, backslashes, control characters, and malformed
// escapes are rejected. Query strings and fragments are supported.
// No request host or forwarding headers are trusted to establish the origin.
func SafeRedirectURL(destination, fallback string) string {
	for _, candidate := range []string{destination, fallback} {
		if !strings.HasPrefix(candidate, "/") || strings.HasPrefix(candidate, "//") {
			continue
		}
		u, err := url.Parse(candidate)
		if err != nil || u.IsAbs() || u.Host != "" || strings.HasPrefix(u.Path, "//") {
			continue
		}

		// Check escaped characters too, including those in queries and fragments.
		decoded, err := url.PathUnescape(candidate)
		if err != nil || strings.ContainsFunc(decoded, func(c rune) bool {
			return c == '\\' || c < 0x20 || c == 0x7f
		}) {
			continue
		}
		return u.String()
	}
	return "/"
}

// RedirectToURL sends a 303 Location redirect for ordinary requests or a 200
// HX-Redirect response for HTMX. Redirects do not replay mutation bodies.
// Invalid destinations fall back to "/". No response body is rendered.
func RedirectToURL(w http.ResponseWriter, r *http.Request, redirectURL string) {
	redirectURL = SafeRedirectURL(redirectURL, "/")
	w.Header().Set("Cache-Control", "no-store")
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", redirectURL)
		w.WriteHeader(http.StatusOK)
	} else {
		w.Header().Set("Location", redirectURL)
		w.WriteHeader(http.StatusSeeOther)
	}
}
