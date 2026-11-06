// Package htmx reads HTMX request headers, writes HTMX response headers, and
// serves optional browser error handling. Apply response metadata before writing
// a response body.
package htmx

import (
	"net/http"
	"net/url"
	"strings"
)

const (
	// HeaderBoosted names the request header indicating an hx-boost request.
	HeaderBoosted = "HX-Boosted"
	// HeaderHistoryRestoreRequest names the request header for HTMX history restoration.
	HeaderHistoryRestoreRequest = "HX-History-Restore-Request"
	// HeaderRequest names the header whose value is true for HTMX requests.
	HeaderRequest = "HX-Request"
	// HeaderRequestType indicates whether HTMX needs a full document or a partial.
	HeaderRequestType = "HX-Request-Type"
	// HeaderTarget names the request header containing the target's tag and ID.
	HeaderTarget = "HX-Target"
	// HeaderTrigger names the response header for client events.
	HeaderTrigger = "HX-Trigger"
	// HeaderSource names the request header containing the source's tag and ID.
	HeaderSource = "HX-Source"
)

const (
	// HeaderPushURL names the response header that pushes a URL into browser history.
	HeaderPushURL = "HX-Push-Url"
	// HeaderRedirect names the response header that requests a full browser redirect.
	HeaderRedirect = "HX-Redirect"
	// HeaderReplaceURL names the response header that replaces the current history URL.
	HeaderReplaceURL = "HX-Replace-Url"
	// HeaderRefresh names the response header that requests a full-page refresh.
	HeaderRefresh = "HX-Refresh"
	// HeaderRetarget names the response header selecting a different swap target.
	HeaderRetarget = "HX-Retarget"
	// HeaderReswap names the response header overriding the swap method.
	HeaderReswap = "HX-Reswap"
)

type (
	// Request contains the HTMX request headers.
	Request struct {
		Enabled        bool
		Boosted        bool
		HistoryRestore bool
		FullPage       bool
		// Source contains the source element's tag and optional URI-encoded ID.
		Source string
		// Target is the target element's ID, without its tag or # prefix.
		Target string
	}

	// Response contains server-selected HTMX response metadata.
	Response struct {
		PushURL    string
		Refresh    bool
		ReplaceURL string
		Retarget   string
		Reswap     string
		Trigger    string

		// NoContent makes Apply commit HTTP 204 after setting the headers.
		NoContent bool
	}
)

// NewResponse returns empty HTMX response metadata.
func NewResponse() *Response {
	return &Response{}
}

// GetRequest reads HTMX request headers. Boolean headers must equal "true".
func GetRequest(r *http.Request) Request {
	_, target, _ := strings.Cut(r.Header.Get(HeaderTarget), "#")
	target, _ = url.PathUnescape(target)
	return Request{
		Enabled:        IsHtmxRequest(r),
		Boosted:        r.Header.Get(HeaderBoosted) == "true",
		FullPage:       r.Header.Get(HeaderRequestType) == "full",
		Source:         r.Header.Get(HeaderSource),
		Target:         target,
		HistoryRestore: r.Header.Get(HeaderHistoryRestoreRequest) == "true",
	}
}

// Apply sets nonempty metadata headers, preserving unrelated headers and fields
// left unset. NoContent commits HTTP 204. Otherwise, Apply leaves the status and
// body unwritten. The caller must decide whether the request supports HTMX.
func (r Response) Apply(w http.ResponseWriter) {
	if r.PushURL != "" {
		w.Header().Set(HeaderPushURL, r.PushURL)
	}
	if r.Refresh {
		w.Header().Set(HeaderRefresh, "true")
	}
	if r.Trigger != "" {
		w.Header().Set(HeaderTrigger, r.Trigger)
	}
	if r.ReplaceURL != "" {
		w.Header().Set(HeaderReplaceURL, r.ReplaceURL)
	}
	if r.Retarget != "" {
		w.Header().Set(HeaderRetarget, r.Retarget)
	}
	if r.Reswap != "" {
		w.Header().Set(HeaderReswap, r.Reswap)
	}
	if r.NoContent {
		w.WriteHeader(http.StatusNoContent)
	}
}

// IsHtmxRequest reports whether HX-Request is true.
func IsHtmxRequest(r *http.Request) bool {
	return r.Header.Get(HeaderRequest) == "true"
}
