package form

import (
	"context"
	"fmt"
	"net/http"
	"reflect"

	"github.com/trentnix/hyperserver/pkg/components/messages"
)

type (
	// FormComponent defines an interface for a form that is rendered to a requestor
	FormComponent interface {
		// IsValidated reports whether validation ran, not whether the form is valid.
		IsValidated() bool
		// SetValidated records whether validation ran.
		SetValidated(bool)

		// GetFieldErrors returns errors for a named field.
		GetFieldErrors(string) []string
		// GetAllFieldErrors returns errors across all fields.
		GetAllFieldErrors() []string
		// SetFieldErrors records field errors from a validation result.
		SetFieldErrors(error)
		// HasFieldErrors reports whether a named field has errors.
		HasFieldErrors(string) bool

		// GetSuccessMessages returns form-level success messages.
		GetSuccessMessages() []messages.Message
		// GetErrorMessages returns form-level errors, excluding field errors.
		GetErrorMessages() []messages.Message
		// GetInfoMessages returns informational messages.
		GetInfoMessages() []messages.Message
		// HasErrorMessages checks only form-level errors.
		HasErrorMessages() bool
		// HasErrors checks both field and form-level errors.
		HasErrors() bool

		// SetMessages replaces the form-level messages.
		SetMessages([]messages.Message)
		// AddErrorMessage records a form-level error.
		AddErrorMessage(string)
		// AddSuccessMessage records a successful operation.
		AddSuccessMessage(string)
		// AddMessage records an informational message.
		AddMessage(string)

		// Bind allows a form to populate its fields from an http.Request
		Bind(*http.Request) error
	}

	contextKey string
)

const (
	// FormKey identifies a FormComponent stored in a context.
	FormKey contextKey = "form"
)

// Validate records struct-tag validation errors on f, which must be a pointer
// to a struct implementing FormComponent. Field validation failures do not produce
// a returned error. Call HasErrors after a nil return before processing the form.
func Validate(f any) error {
	validate := NewValidator()

	// Ensure f is a pointer to a struct
	val := reflect.ValueOf(f)
	if val.Kind() != reflect.Ptr || val.Elem().Kind() != reflect.Struct {
		return NewErrInvalidForm(fmt.Errorf("The form being validated should be passed as a reference and not by value."))
	}

	// Extract the concrete struct value and validate
	structValue := val.Elem().Interface()
	err := validate.Validate(structValue)

	// Check if f implements FormComponent
	if formComponent, ok := f.(FormComponent); ok {
		// Use the FormComponent methods to set validation state
		formComponent.SetValidated(true)
		if err != nil {
			formComponent.SetFieldErrors(err)
		}
	} else {
		return NewErrFormComponentInterfaceNotImplemented(fmt.Errorf("The specified form does not implement the FormComponent interface."))
	}

	return nil
}

// AddFormToContext takes the specified form and saves a pointer to the form to
// the provided context
func AddFormToContext(ctx context.Context, f any) (context.Context, error) {
	if f == nil {
		return ctx, nil
	}

	var formPtr any

	val := reflect.ValueOf(f)
	if val.Kind() != reflect.Ptr {
		// Create a pointer to the value
		ptrVal := reflect.New(val.Type())
		ptrVal.Elem().Set(val)
		formPtr = ptrVal.Interface()
	} else {
		// form is already a pointer
		formPtr = f
	}

	if _, ok := f.(FormComponent); !ok {
		return ctx, NewErrFormComponentInterfaceNotImplemented(fmt.Errorf("The specified form does not implement the FormComponent interface."))
	}

	return context.WithValue(ctx, FormKey, formPtr), nil
}

// GetFormFromContext retrieves any form saved to the specified context, confirms it
// implements the FormComponent interface, and if so, returns the form
func GetFormFromContext(ctx context.Context) (any, error) {
	f := ctx.Value(FormKey)
	if f == nil {
		return nil, nil
	}

	// Check if f implements FormComponent
	if _, ok := f.(FormComponent); !ok {
		return nil, NewErrFormComponentInterfaceNotImplemented(fmt.Errorf("The object stored in the request context does not implement FormComponent"))
	}

	return f, nil
}

// ParseAndValidate parses request data, binds f, and records validation errors.
// The returned message and error describe parsing, binding, or form-setup failures.
// A nil error does not mean the input is valid: callers must also check f.HasErrors().
func ParseAndValidate(r *http.Request, f FormComponent) (string, error) {
	if err := r.ParseForm(); err != nil {
		return "Unable to parse form data", err
	}

	if err := f.Bind(r); err != nil {
		return "Unable to save form data", err
	}

	// Validate the form using the package-level function.
	if err := Validate(f); err != nil {
		return "Unable to validate form data", err
	}

	return "", nil
}
