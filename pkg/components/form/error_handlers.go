// error_handlers.go defines error handlers for the form package
package form

import (
	"fmt"
	"net/http"
	"reflect"

	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/util"
)

// HandleFormError is a generic handler to render the specified content with the specified form
// to display the specified error. If an error is specified, it will be logged.
func HandleFormError(
	w http.ResponseWriter,
	r *http.Request,
	c *content.Content,
	f FormComponent,
	formErrorMessage string,
	e error,
) {
	if formErrorMessage != "" {
		f.AddErrorMessage(formErrorMessage)
	}

	if e != nil {
		formType := reflect.TypeOf(f)
		logger.LogRequestError(r, fmt.Errorf("form (%s) error: %s: %w", formType, formErrorMessage, e))
	}

	c.Data = f

	if err := c.Render(w, r); err != nil {
		util.HttpError(w, r, formErrorMessage, err, http.StatusInternalServerError)
	}
}
