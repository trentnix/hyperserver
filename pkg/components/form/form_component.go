// form_component.go defines the FormComponent interface and functions that use the interface
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
		// specifies whether the form has been validated (but not whether any errors were found)
		IsValidated() bool
		// sets the form status to having been validated or not
		SetValidated(bool)

		// gets the errors messages for the specified field
		GetFieldErrors(string) []string
		// gets all the field error messages in a single slice
		GetAllFieldErrors() []string
		// sets the form field error messages
		SetFieldErrors(error)
		// determines whether a specific field has errors
		HasFieldErrors(string) bool

		// gets form-level success messages
		GetSuccessMessages() []messages.Message
		// gets form-level error messages
		GetErrorMessages() []messages.Message
		// gets form-level informational messages
		GetInfoMessages() []messages.Message
		// determines whether the form has error messages
		HasErrorMessages() bool
		// determines whether field or form-level errors prevent submission
		HasErrors() bool

		// sets a form-level non-error messages
		SetMessages([]messages.Message)
		// adds an error message
		AddErrorMessage(string)
		// adds a success message
		AddSuccessMessage(string)
		// adds an informational message
		AddMessage(string)

		// Bind allows a form to populate its fields from an http.Request
		Bind(*http.Request) error
	}

	contextKey string
)

const (
	// defines the key that serializes a form to and from a context
	FormKey contextKey = "form"
)

// Validate takes the specified form, confirms it implements the FormComponent interface,
// and validates it according to any struct-field validation attributes that were provided in
// the form struct definition
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

// ParseAndValidate combines the parsing, binding, and validation of the
// specified form into a single call
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
