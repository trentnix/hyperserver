package form

import (
	"fmt"
	"net/http"
	"reflect"
)

type (
	// ValidationForm checks input and records validation results. It does not
	// require HTTP binding, messages, or template-facing accessors.
	ValidationForm interface {
		// Validate checks input without changing form state or application data.
		Validate() FieldErrors
		// SetValidated records whether validation ran.
		SetValidated(bool)
		// SetFieldErrors records field errors from a validation result.
		SetFieldErrors(FieldErrors)
	}

	// FormComponent supplies the binding and validation used by ParseAndValidate.
	// Embed Form for the standard error storage and template helpers, or supply
	// those methods independently. Rendering does not require this interface.
	FormComponent interface {
		ValidationForm
		// Bind allows a form to populate its fields from an http.Request
		Bind(*http.Request) error
	}
)

// Validate records the field errors returned by f.Validate. Nil forms are rejected.
// Field validation failures do not produce a returned error. Check the recorded
// field errors before processing the form.
func Validate(f ValidationForm) error {
	if isNilForm(f) {
		return NewErrInvalidForm(fmt.Errorf("the form must not be nil"))
	}

	f.SetFieldErrors(f.Validate())
	f.SetValidated(true)
	return nil
}

// An interface can hold a typed nil. Inspect only nil-capable kinds so valid
// value implementations do not need a particular representation.
func isNilForm(f ValidationForm) bool {
	if f == nil {
		return true
	}
	value := reflect.ValueOf(f)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// ParseAndValidate parses request data with the default field limits, binds f,
// and records validation errors. Use ParseAndValidateWithOptions for other limits.
// The returned message and error describe parsing, binding, or form-setup failures.
// A nil error does not mean the input is valid: callers must check recorded errors
// (for forms embedding Form, call HasErrors).
func ParseAndValidate(r *http.Request, f FormComponent) (string, error) {
	return ParseAndValidateWithOptions(r, f, ParseOptions{})
}

// ParseAndValidateWithOptions parses and validates with explicit field limits.
// Callers must still check recorded errors after a nil error.
func ParseAndValidateWithOptions(r *http.Request, f FormComponent, options ParseOptions) (string, error) {
	if err := ParseWithOptions(r, options); err != nil {
		return err.Error(), err
	}
	if isNilForm(f) {
		return "Unable to validate form data", NewErrInvalidForm(fmt.Errorf("the form must not be nil"))
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
