// redirect.go contains utility functions related to redirecting a requestor to
// a specified URL
package content

import "net/http"

// RedirectToURL redirects the requestor to the prodivided URL
func RedirectToURL(w http.ResponseWriter, r *http.Request, redirectURL string) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", redirectURL)
		w.WriteHeader(http.StatusOK)
	} else {
		http.Redirect(w, r, redirectURL, http.StatusFound)
	}
}
