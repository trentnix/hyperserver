package content

import (
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/trentnix/hyperserver/pkg/components/htmx"
	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/util"
)

// ResponseTemplates selects named partials for shared responses. All three names
// are required and can refer to the same partial. Templates receive the public
// message as .Data, never the underlying error.
type ResponseTemplates struct {
	Message, Error, NotFound string
	// NotFoundMessage defaults to http.StatusText(http.StatusNotFound).
	NotFoundMessage string
}

// ConfigureResponses installs standard message, error, and not-found handlers.
// Call during application setup, before explicit handler overrides. No files are
// read until handlers.Setup calls cm.PrepareTemplates after layout overrides.
// An existing preparation callback is rejected rather than replaced.
//
// Responses use the configured page or HTMX layout and Cache-Control: no-store.
// They set the HTML content type and discard any preexisting content length.
// Errors bypass notification storage, log their cause, and accept statuses
// 400–599 (other statuses become 500). Callers must sanitize logged causes.
// Rendering failures use a plain-text 500 without calling HandleError recursively.
// Direct users must call PrepareTemplates before serving. Do not reconfigure or
// prepare while serving requests.
func ConfigureResponses(cm *contentservice.ContentManagerService, selection ResponseTemplates, files ...TemplatePath) error {
	if cm == nil {
		return fmt.Errorf("response templates require a content manager")
	}
	if cm.PrepareTemplates != nil {
		return fmt.Errorf("shared template preparation is already configured")
	}
	if selection.Message == "" || selection.Error == "" || selection.NotFound == "" {
		return fmt.Errorf("message, error, and not-found partial names are required")
	}
	if selection.NotFoundMessage == "" {
		selection.NotFoundMessage = http.StatusText(http.StatusNotFound)
	}
	files = slices.Clone(files)
	var views map[string]*View
	cm.PrepareTemplates = func() error {
		templates, err := PrepareTemplates(cm, files...)
		if err != nil {
			return err
		}
		prepared := make(map[string]*View)
		for _, name := range []string{selection.Message, selection.Error, selection.NotFound} {
			view, err := templates.View(cm, name)
			if err != nil {
				return err
			}
			prepared[name] = view
		}
		views = prepared
		return nil
	}
	cm.HandleMessage = func(w http.ResponseWriter, r *http.Request, message string) {
		renderStandardResponse(w, r, views[selection.Message], message, http.StatusOK)
	}
	cm.HandleError = func(w http.ResponseWriter, r *http.Request, message string, cause error, status int) {
		if status < 400 || status > 599 {
			status = http.StatusInternalServerError
		}
		logger.LogRequestError(r, cause)
		if message == "" {
			message = http.StatusText(status)
		}
		renderStandardResponse(w, r, views[selection.Error], message, status)
	}
	cm.HandleNotFound = func(w http.ResponseWriter, r *http.Request) {
		renderStandardResponse(w, r, views[selection.NotFound], selection.NotFoundMessage, http.StatusNotFound)
	}
	return nil
}

func renderStandardResponse(w http.ResponseWriter, r *http.Request, view *View, message string, status int) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Del("Content-Length")
	util.AddVary(w.Header(), htmx.HeaderRequest, htmx.HeaderHistoryRestoreRequest)

	var err error
	if view == nil {
		err = fmt.Errorf("shared response templates have not been prepared")
	} else {
		page := view.Content(r, message)
		page.ResponseStatusCode = status
		if status >= 400 {
			// A failed notification store must not prevent an error response.
			manager := *page.ContentManager
			manager.LoadNotifications = nil
			page.ContentManager = &manager
		}
		err = page.Render(w, r)
	}
	if err == nil {
		return
	}
	logger.LogRequestError(r, err)
	if errors.Is(err, ErrDeliveringResponse) {
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Del("Content-Length")
	w.WriteHeader(http.StatusInternalServerError)
	if r.Method != http.MethodHead {
		if _, err := w.Write([]byte("Unable to display this page.\n")); err != nil {
			logger.LogRequestError(r, err)
		}
	}
}
