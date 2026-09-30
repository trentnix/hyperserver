package util

import (
	"fmt"
	"net/http"
)

// RedirectToURL returns HTTP 200 with HX-Redirect for HTMX requests or an HTML
// redirect page otherwise. It does not validate or escape redirectURL, which must
// be a trusted destination. It does not issue a standard HTTP redirect status.
func RedirectToURL(w http.ResponseWriter, r *http.Request, redirectURL string) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", redirectURL)
		w.WriteHeader(http.StatusOK)
	} else {
		// The current fallback uses an HTML page rather than an HTTP redirect.
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		redirectPage := fmt.Sprintf(`
<html>
  <head>
    <meta http-equiv="refresh" content="0; url=%s">
  </head>
  <body>
    <script>window.location.href = "%s";</script>
    <p>If you are not redirected automatically, <a href="%s">click here</a>.</p>
  </body>
</html>`, redirectURL, redirectURL, redirectURL)
		fmt.Fprint(w, redirectPage)
	}
}
