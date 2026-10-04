package form

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"

	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/util"
)

// HandleFormError is a generic handler to render the specified content with the specified form
// to display the specified error. ParseError returns a plain-text rejection
// without rendering submitted values. Other errors are logged before rendering.
func HandleFormError(
	w http.ResponseWriter,
	r *http.Request,
	c *content.Content,
	f FormComponent,
	formErrorMessage string,
	e error,
) {
	var parseErr *ParseError
	if errors.As(e, &parseErr) {
		util.HttpError(w, r, parseErr.Error(), nil, parseErr.StatusCode)
		return
	}

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
