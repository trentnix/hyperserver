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

// ErrorForm accepts a form-level error for rendering. Binding and validation are
// not required. The template determines which fields and accessors it needs.
type ErrorForm interface {
	AddErrorMessage(string)
}

// HandleFormError is a generic handler to render the specified content with the specified form
// to display the specified error. ParseError returns a plain-text rejection
// without rendering submitted values. FieldErrors render recorded field messages
// without logging or adding a form-level message. Other errors are logged.
// Rendering failures use ContentManager.HandleError when configured, or plain text.
func HandleFormError(
	w http.ResponseWriter,
	r *http.Request,
	c *content.Content,
	f ErrorForm,
	formErrorMessage string,
	e error,
) {
	var parseErr *ParseError
	if errors.As(e, &parseErr) {
		util.HttpError(w, r, parseErr.Error(), nil, parseErr.StatusCode)
		return
	}
	var fields FieldErrors
	if errors.As(e, &fields) {
		formErrorMessage, e = "", nil
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
		c.HandleRenderError(w, r, formErrorMessage, err)
	}
}
