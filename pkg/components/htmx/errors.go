package htmx

import (
	_ "embed"
	"net/http"
	"strings"
	"time"
)

//go:embed errors.js
var errorScript string

// ServeErrorScript serves the optional HTMX error-handling script. Register a GET
// route and load it once after HTMX with a deferred script tag. HTML 4xx/5xx
// responses swap into their target. Other failures leave existing inputs intact
// and display a text-only alert in the submitting form or dialog, then fall back
// to [data-request-errors] or the document body. Style [data-request-error] in the
// application stylesheet. HTTP failures remain failures in HTMX request events.
func ServeErrorScript(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "htmx-errors.js", time.Time{}, strings.NewReader(errorScript))
}
