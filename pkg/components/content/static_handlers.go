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
		httpError(
			w,
			r,
			message,
			NewErrContentManagerUnavailable(fmt.Errorf("no content manager when handling message")),
			http.StatusInternalServerError)
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
			logger.LogRequestError(r, NewErrHandlerReferencesSelf(nil, "HandleMessage"))
			httpMessage(w, r, message)
		}

		return
	}
}

// httpNotFound is a fallback when a requested resource is not found
func httpNotFound(w http.ResponseWriter, r *http.Request) {
	httpStatus := http.StatusNotFound
	message := fmt.Sprintf("the resource requested ('%s') was not found", r.URL.Path)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(httpStatus)
	_, err := w.Write([]byte(message))
	if err != nil {
		logger.LogRequestError(r, fmt.Errorf("%s (%d): %w", message, httpStatus, err))
	}
}

// HandleNotFound is a static http.HandlerFunc to deal with 404 errors
func HandleNotFound(w http.ResponseWriter, r *http.Request) {
	if r == nil {
		errMessage := "The requested resource was not found"
		httpError(w, r, errMessage, NewErrRequestNotSet(nil), http.StatusInternalServerError)
		return
	}

	errNotFound := fmt.Errorf("the resource requested ('%s') was not found", r.URL.Path)
	logger.LogRequestError(r, NewErrResourceNotFound(errNotFound))

	contentManager := GetContentManager()
	if contentManager == nil {
		httpError(
			w,
			r,
			errNotFound.Error(),
			NewErrContentManagerUnavailable(fmt.Errorf("no content manager when handling message")),
			http.StatusInternalServerError)
		return
	}

	if htmx.IsHtmxRequest(r) && contentManager.NotFoundURL != "" {
		// set a message redirect the user to the 404 handler
		RedirectToURL(w, r, contentManager.NotFoundURL)
	} else {
		// make sure this won't become an infinite loop where the error handler in the
		// content manager was set to this function since they share the same signature
		if reflect.ValueOf(contentManager.HandleNotFound).Pointer() != reflect.ValueOf(HandleNotFound).Pointer() {
			contentManager.HandleNotFound(w, r)
		} else {
			logger.LogRequestError(r, NewErrHandlerReferencesSelf(nil, "HandleNotFound"))
			httpNotFound(w, r)
		}

		return
	}
}
