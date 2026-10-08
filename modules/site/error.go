package module_site

import (
	"bytes"
	"fmt"
	"net/http"

	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/messages"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/util"
)

// Error displays queued error notifications with HTTP 200. Notification retrieval
// or rendering failures return 500. Current failures must use HandleError instead.
func (m *SiteModule) Error(w http.ResponseWriter, r *http.Request) {
	notifications, err := messages.GetNotifications(w, r)
	if err != nil {
		m.HandleError(w, r, "Unable to load error notifications", err, http.StatusInternalServerError)
		return
	}

	var errorMessages []messages.Notification
	for _, n := range notifications {
		if n.IsError() {
			errorMessages = append(errorMessages, n)
		}
	}
	m.renderError(w, r, errorMessages, http.StatusOK)
}

// HandleError logs err and renders only the public message with httpStatus.
// Callers must remove secrets from err before passing it and must not also log it.
// Pass nil for errors already logged in sanitized form. No session is required.
// Status codes outside 400–599 become 500. Call before writing a response.
func (m *SiteModule) HandleError(w http.ResponseWriter, r *http.Request, message string, err error, httpStatus int) {
	if httpStatus < 400 || httpStatus > 599 {
		httpStatus = http.StatusInternalServerError
	}
	if err != nil {
		logger.LogRequestError(r, fmt.Errorf("HTTP %d: %w", httpStatus, err))
	}
	if message == "" {
		message = http.StatusText(httpStatus)
	}
	m.renderError(w, r, []messages.Notification{messages.NewNotification(message, messages.NotificationTypeError)}, httpStatus)
}

func (m *SiteModule) renderError(w http.ResponseWriter, r *http.Request, notifications []messages.Notification, status int) {
	// Error responses must still render when notification storage is unavailable.
	cm := *m.contentManager
	cm.RenderNotifications = false
	page := content.NewManagedContent(r, &cm)
	page.PartialName, page.Title = errorPagePartialName, "Error"
	page.AddContent(errorPageTemplate)
	page.Data = notifications

	// Stage the error page separately to control its status, headers, and HEAD body.
	// Rendering failure uses plain text without recursively invoking HandleError.
	buffer := &errorPageBuffer{headers: make(http.Header)}
	w.Header().Set("Cache-Control", "no-store")
	if err := page.Render(buffer, r); err != nil {
		util.HttpError(w, r, "Unable to display the error page", err, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		if _, err := w.Write(buffer.Bytes()); err != nil {
			logger.LogRequestError(r, err)
		}
	}
}

// errorPageBuffer delays response writes until rendering succeeds. The caller
// selects the status and headers after rendering, not the template renderer.
type errorPageBuffer struct {
	bytes.Buffer
	headers http.Header
}

func (b *errorPageBuffer) Header() http.Header { return b.headers }
func (*errorPageBuffer) WriteHeader(int)       {}
