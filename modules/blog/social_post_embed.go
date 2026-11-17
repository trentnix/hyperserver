package blog

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/trentnix/hyperserver/pkg/services/logger"
)

var socialCallbackID = regexp.MustCompile(`^[a-f0-9]{32}$`)

// Keep arbitrary Mastodon frame origins out of the blog's document policy. This
// small same-origin document can frame only the selected public instance.
func (m *module) mastodonEmbed(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	u, err := linkURL(query.Get("url"))
	if err != nil || len(query["url"]) != 1 || len(query["id"]) != 1 || !socialCallbackID.MatchString(query.Get("id")) {
		http.Error(w, "Invalid Mastodon embed", http.StatusBadRequest)
		return
	}
	canonical, err := mastodonPostURL(u)
	if err != nil {
		http.Error(w, "Invalid Mastodon embed", http.StatusBadRequest)
		return
	}
	origin := (&url.URL{Scheme: "https", Host: strings.ToLower(u.Hostname())}).String()
	body, err := m.views["mastodonEmbed"].RenderPartial(r, struct{ URL, Origin, ID string }{canonical + "/embed", origin, query.Get("id")})
	if err != nil {
		logger.LogRequestError(r, err)
		http.Error(w, "Unable to display the Mastodon post", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; style-src-attr 'unsafe-inline'; frame-src "+origin+"; base-uri 'none'; form-action 'none'; frame-ancestors 'self'")
	w.Header().Set("Cache-Control", "no-store")
	w.Write([]byte(body))
}
