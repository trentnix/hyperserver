// Package htmx provides HTMX request and response header helpers.
// This package duplicates pkg/components/htmx. The rendering components currently
// use pkg/components/htmx.
package htmx

import (
	"net/http"
)

const (
	// HeaderBoosted names the request header indicating an hx-boost request.
	HeaderBoosted = "HX-Boosted"
	// HeaderHistoryRestoreRequest names the request header for an HTMX history-cache miss.
	HeaderHistoryRestoreRequest = "HX-History-Restore-Request"
	// HeaderPrompt names the request header containing an hx-prompt answer.
	HeaderPrompt = "HX-Prompt"
	// HeaderRequest names the header whose value is true for HTMX requests.
	HeaderRequest = "HX-Request"
	// HeaderTarget names the request header containing the target element ID.
	HeaderTarget = "HX-Target"
	// HeaderTrigger names the request header for the triggering element and the response header for client events.
	HeaderTrigger = "HX-Trigger"
	// HeaderTriggerName names the request header containing the triggering element's name.
	HeaderTriggerName = "HX-Trigger-Name"
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
	// HeaderTriggerAfterSettle names the response header for events after HTMX settles.
	HeaderTriggerAfterSettle = "HX-Trigger-After-Settle"
	// HeaderTriggerAfterSwap names the response header for events after an HTMX swap.
	HeaderTriggerAfterSwap = "HX-Trigger-After-Swap"
)

type (
	// Request contains data that HTMX provides during requests
	Request struct {
		Enabled        bool
		Boosted        bool
		HistoryRestore bool
		Trigger        string
		TriggerName    string
		Target         string
		Prompt         string
	}

	// Response contain data that the server can communicate back to HTMX
	Response struct {
		PushURL            string
		Refresh            bool
		ReplaceURL         string
		Trigger            string
		TriggerAfterSwap   string
		TriggerAfterSettle string
		NoContent          bool
	}
)

// NewResponse returns empty HTMX response metadata.
func NewResponse() *Response {
	return &Response{}
}

// GetRequest extracts HTMX data from the request
func GetRequest(r *http.Request) Request {
	return Request{
		Enabled:        r.Header.Get(HeaderRequest) == "true",
		Boosted:        r.Header.Get(HeaderBoosted) == "true",
		Trigger:        r.Header.Get(HeaderTrigger),
		TriggerName:    r.Header.Get(HeaderTriggerName),
		Target:         r.Header.Get(HeaderTarget),
		Prompt:         r.Header.Get(HeaderPrompt),
		HistoryRestore: r.Header.Get(HeaderHistoryRestoreRequest) == "true",
	}
}

// Apply applies data from a Response to a server response
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
	if r.TriggerAfterSwap != "" {
		w.Header().Set(HeaderTriggerAfterSwap, r.TriggerAfterSwap)
	}
	if r.TriggerAfterSettle != "" {
		w.Header().Set(HeaderTriggerAfterSettle, r.TriggerAfterSettle)
	}
	if r.ReplaceURL != "" {
		w.Header().Set(HeaderReplaceURL, r.ReplaceURL)
	}
	if r.NoContent {
		w.WriteHeader(http.StatusNoContent)
	}
}

// IsHtmxRequest reports whether HX-Request is true.
func IsHtmxRequest(r *http.Request) bool {
	return r.Header.Get(HeaderRequest) == "true"
}
