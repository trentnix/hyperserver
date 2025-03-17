// error_handlers.go contains error handlers that can be used throughout the application
package content

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"

	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/components/htmx"
	"github.com/trentnix/hyperserver/pkg/components/messages"
	"github.com/trentnix/hyperserver/pkg/services/logger"
)

// httpError calls http.Error instead of using a customer error handler
func httpError(w http.ResponseWriter, r *http.Request, message string, err error, httpStatusCode int) {
	errMessage := message
	if err != nil {
		errMessage = fmt.Sprintf("%s: %v", message, err)
	}

	http.Error(w, errMessage, httpStatusCode)
}

// HandleError calls the registered error handler if a ContentManager exists. Otherwise, it
// falls back to use the DefaultErrorHandler.
func HandleError(w http.ResponseWriter, r *http.Request, message string, err error, httpStatus int) {
	if r == nil {
		errMessage := fmt.Sprintf("the http request is not set when reporting the following issue: %s", message)
		httpError(w, r, errMessage, NewErrRequestNotSet(nil), http.StatusInternalServerError)
		return
	}

	logger.LogRequestError(r, err)

	var parseErr *ErrParsingTemplates
	if errors.As(err, &parseErr) {
		// err is of type *ErrParsingTemplates so calling the default error handler
		// will result in an endless loop trying to render any potential error pages.
		// Need to use a standard http error in this case.
		httpError(w, r, message, err, http.StatusInternalServerError)
		return
	}

	contentManager := GetContentManager()
	if contentManager == nil {
		http.Error(w, message, http.StatusInternalServerError)
		return
	}

	if htmx.IsHtmxRequest(r) {
		// set a message redirect the user to the 404 handler
		errAddMessage := messages.AddErrorMessage(w, r, message)
		if errAddMessage != nil {
			logger.LogRequestError(r, fmt.Errorf("there was an error adding an error message before redirecting to the error page: %w", errAddMessage))
		}

		RedirectToURL(w, r, contentManager.ErrorURL)
	} else {
		// make sure this won't become an infinite loop where the error handler in the
		// content manager was set to this function since they share the same signature
		if reflect.ValueOf(contentManager.HandleError).Pointer() != reflect.ValueOf(HandleError).Pointer() {
			contentManager.HandleError(w, r, message, err, httpStatus)
		} else {
			http.Error(w, message, http.StatusInternalServerError)
		}

		return
	}
}

// HandleFormError is a generic handler to render the specified content with the specified form
// to display the specified error
func HandleFormError(w http.ResponseWriter, r *http.Request, c *Content, f form.FormComponent, formErrorMessage string) {
	if formErrorMessage != "" {
		f.SetFormError(formErrorMessage)
	}

	c.Data = f

	if err := c.Render(w, r); err != nil {
		contentManager := GetContentManager()
		if contentManager == nil {
			httpError(w, r, formErrorMessage, err, http.StatusInternalServerError)
			return
		}

		errMessage := fmt.Sprintf("there was an error rendering the specified form: %v", err)
		logger.LogRequestError(r, fmt.Errorf("%s", errMessage))
		contentManager.HandleError(w, r, errMessage, err, http.StatusInternalServerError)
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
		http.Error(w, errNotFound.Error(), http.StatusNotFound)
	}

	if htmx.IsHtmxRequest(r) {
		// set a message redirect the user to the 404 handler
		RedirectToURL(w, r, contentManager.NotFoundURL)
	} else {
		contentManager.HandleNotFound(w, r)
		return
	}
}
