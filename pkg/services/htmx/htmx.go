// htmx.go defines the htmx.Request and htmx.Response structures that will be
// used for managing site content when an HTMX interaction is necessary
package htmx

import (
	"net/http"
)

const (
	// indicates that the request is via an element using hx-boost
	HeaderBoosted = "HX-Boosted"
	// “true” if the request is for history restoration after a miss in the local history cache
	HeaderHistoryRestoreRequest = "HX-History-Restore-Request"
	// the user response to an hx-prompt
	HeaderPrompt = "HX-Prompt"
	// always “true”
	HeaderRequest = "HX-Request"
	// the id of the target element if it exists
	HeaderTarget = "HX-Target"
	// the id of the triggered element if it exists
	HeaderTrigger = "HX-Trigger"
	// the name of the triggered element if it exists
	HeaderTriggerName = "HX-Trigger-Name"
)

const (
	// pushes a new url into the history stack
	HeaderPushURL = "HX-Push-Url"
	// can be used to do a client-side redirect to a new location
	HeaderRedirect = "HX-Redirect"
	// replaces the current URL in the location bar
	HeaderReplaceURL = "HX-Replace-Url"
	// if set to “true” the client-side will do a full refresh of the page
	HeaderRefresh = "HX-Refresh"
	// allows you to trigger client-side events after the settle step
	HeaderTriggerAfterSettle = "HX-Trigger-After-Settle"
	// allows you to trigger client-side events after the swap step
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

// IsHTMXRequest checks if the given HTTP request is an HTMX request.
func IsHtmxRequest(r *http.Request) bool {
	return r.Header.Get(HeaderRequest) == "true"
}
