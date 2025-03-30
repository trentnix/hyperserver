package content

import (
	"fmt"
	"net/http"
	"reflect"

	"github.com/trentnix/hyperserver/pkg/components/htmx"
	"github.com/trentnix/hyperserver/pkg/components/messages"
	"github.com/trentnix/hyperserver/pkg/services/logger"
)

// httpMessage writes a text message to the ResponseWriter
func httpMessage(w http.ResponseWriter, r *http.Request, message string) {
	httpStatus := http.StatusOK

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(httpStatus)
	_, err := w.Write([]byte(message))
	if err != nil {
		logger.LogRequestError(r, fmt.Errorf("%s (%d): %w", message, httpStatus, err))
	}
}

// HandleMessage writes the specified message using the default message handler
func HandleMessage(w http.ResponseWriter, r *http.Request, message string) {
	if r == nil {
		httpMessage(w, r, message)
		return
	}

	contentManager := GetContentManager()
	if contentManager == nil {
		http.Error(w, message, http.StatusInternalServerError)
		return
	}

	if htmx.IsHtmxRequest(r) && contentManager.MessageURL != "" {
		// redirect the user to the error handler
		errAddMessage := messages.AddErrorMessage(w, r, message)
		if errAddMessage != nil {
			logger.LogRequestError(r, errAddMessage)
		}

		RedirectToURL(w, r, contentManager.MessageURL)
	} else {
		// make sure this won't become an infinite loop where the error handler in the
		// content manager was set to this function since they share the same signature
		if reflect.ValueOf(contentManager.HandleMessage).Pointer() != reflect.ValueOf(HandleMessage).Pointer() {
			contentManager.HandleMessage(w, r, message)
		} else {
			http.Error(w, message, http.StatusInternalServerError)
		}

		return
	}
}
