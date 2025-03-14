// error_handlers.go contains error handlers that can be used throughout the application
package content

import (
	"fmt"
	"net/http"

	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/services/logger"
)

// defaultErrorHandler provides a default error handler to use if a module doesn't
// define its own error handler for the application
func DefaultErrorHandler(w http.ResponseWriter, r *http.Request, message string, err error) {
	errMessage := message
	if err != nil {
		errMessage = fmt.Sprintf("%s: %v", message, err)
	}

	logger.LogRequestError(r, errMessage, err)
	http.Error(w, errMessage, http.StatusInternalServerError)
}

// HandleRenderError logs an error that occurs when rendering fails and the error is reported
// to the client
func HandleRenderingError(w http.ResponseWriter, r *http.Request, message string, err error) {
	errMessage := message
	if err != nil {
		errMessage = fmt.Sprintf("%s: %v", message, err)
	}

	DefaultErrorHandler(w, r, errMessage, nil)
}

// HandleError calls the registered error handler if a ContentManager exists. Otherwise, it
// falls back to use the DefaultErrorHandler.
func HandleError(w http.ResponseWriter, r *http.Request, message string, err error) {
	contentManager := GetContentManager()
	if contentManager == nil {
		DefaultErrorHandler(w, r, message, err)
		return
	}

	contentManager.ErrorHandler(w, r, message, err)
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
			DefaultErrorHandler(w, r, formErrorMessage, err)
			return
		}

		errMessage := fmt.Sprintf("there was an error rendering the specified form: %v", err)
		contentManager.ErrorHandler(w, r, errMessage, err)
	}
}
