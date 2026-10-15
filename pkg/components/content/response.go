package content

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/trentnix/hyperserver/pkg/components/htmx"
	"github.com/trentnix/hyperserver/pkg/components/messages"
	"github.com/trentnix/hyperserver/pkg/util"
)

// Render dispatches a page, HTMX fragment, or local redirect using Content as the
// response description. RedirectURL takes precedence and skips notification loading
// and rendering. HTMX.Response and OutOfBand apply only to HTMX requests.
// For HTMX, NoContent skips rendering and notification loading and sends HTTP 204.
// Otherwise, templates and out-of-band updates finish before response headers or
// body are written. Notifications are acknowledged after rendering succeeds and
// before headers are written. HEAD and empty bodies do not acknowledge notifications.
// HTMX refresh responses skip notification loading because the browser discards their body.
// Delivery errors can follow partial writes. Call before committing the response.
// Use http.ResponseWriter directly for streaming and other specialized responses.
// History restoration renders a full page without fragment response metadata.
// Responses vary on HX-Request and HX-History-Restore-Request and default to no-store.
// An existing cache policy takes precedence over Content.Headers.
// ResponseStatusCode defaults to 200.
func (c *Content) Render(w http.ResponseWriter, r *http.Request) error {
	if c.RedirectURL != "" {
		util.RedirectToURL(w, r, c.RedirectURL)
		return nil
	}

	view := *c
	view.HTMX.Request = htmx.GetRequest(r)
	metadata := view.HTMX.Response
	status := view.ResponseStatusCode
	if status == 0 {
		status = http.StatusOK
	}
	if view.IsHtmx() && metadata != nil && metadata.NoContent {
		status = http.StatusNoContent
	}
	if status < 200 || status >= 600 || status >= 300 && status < 400 {
		return fmt.Errorf("unsupported content response status %d: use RedirectURL for redirects", status)
	}
	if status == http.StatusNoContent {
		view.applyHeaders(w)
		w.WriteHeader(status)
		return nil
	}
	if status == http.StatusResetContent {
		return fmt.Errorf("content response status %d does not allow a body", status)
	}

	var acknowledge func(http.ResponseWriter) error
	refresh := view.IsHtmx() && metadata != nil && metadata.Refresh
	if r.Method != http.MethodHead && !refresh && view.ContentManager != nil && view.ContentManager.LoadNotifications != nil {
		notifications, ack, err := view.ContentManager.LoadNotifications(r)
		if err != nil {
			return err
		}
		view.Notifications = append(append([]messages.Notification(nil), c.Notifications...), notifications...)
		acknowledge = ack
	}
	body, err := view.render()
	if err != nil {
		return err
	}
	if body.Len() > 0 && acknowledge != nil {
		if err := acknowledge(w); err != nil {
			return err
		}
	}

	view.applyHeaders(w)
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return nil
	}
	if _, err := body.WriteTo(w); err != nil {
		return NewErrRenderingTemplates(err)
	}
	return nil
}

// applyHeaders runs only after rendering succeeds, or for a bodyless response.
func (c *Content) applyHeaders(w http.ResponseWriter) {
	for k, v := range c.Headers {
		switch {
		case strings.EqualFold(k, "Vary"):
			util.AddVary(w.Header(), v)
		case strings.EqualFold(k, "Cache-Control"):
			if w.Header().Get("Cache-Control") == "" {
				w.Header().Set(k, v)
			}
		default:
			w.Header().Set(k, v)
		}
	}
	util.AddVary(w.Header(), htmx.HeaderRequest, htmx.HeaderHistoryRestoreRequest)
	if w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "no-store")
	}
	if c.IsHtmx() && c.HTMX.Response != nil {
		metadata := *c.HTMX.Response
		metadata.NoContent = false // The dispatcher owns the status and body.
		metadata.Apply(w)
	}
}
