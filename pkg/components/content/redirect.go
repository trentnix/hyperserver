// redirect.go contains utility functions related to redirecting a requestor to a given URL
package content

import (
	"fmt"
	"net/http"
)

// RedirectToURL redirects the requestor to the prodivided URL
func RedirectToURL(w http.ResponseWriter, r *http.Request, redirectURL string) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", redirectURL)
		w.WriteHeader(http.StatusOK)
	} else {
		// Some browsers restrict 302 redirects for security or policy reasons. As
		// a result, we will a page directly to the ResponseWriter to use client-side
		// script to handle the redirect.
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
