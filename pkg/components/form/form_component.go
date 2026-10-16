package form

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"

	hs_errors "github.com/trentnix/hyperserver/pkg/errors"
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
		// Bind populates fields from the request. It can return FieldErrors
		// when input cannot be converted to a field's type.
		Bind(*http.Request) error
	}
)

// Validate records and returns field errors. Nil means every field passed.
// Existing form-level messages remain unchanged. Nil forms are rejected.
func Validate(f ValidationForm) error {
	if isNilForm(f) {
		return hs_errors.WithCause(ErrInvalidForm, fmt.Errorf("the form must not be nil"))
	}

	fields := f.Validate()
	f.SetFieldErrors(fields)
	f.SetValidated(true)
	for _, messages := range fields {
		if len(messages) > 0 {
			return fields
		}
	}
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
// A nil error means parsing, binding, and field validation succeeded. Invalid
// fields return FieldErrors, which HandleFormError renders without logging them.
// FieldErrors from Bind are recorded without running validation.
func ParseAndValidate(r *http.Request, f FormComponent) error {
	return ParseAndValidateWithOptions(r, f, ParseOptions{})
}

// ParseAndValidateWithOptions parses and validates with explicit field limits.
// Its result has the same guarantees as ParseAndValidate.
func ParseAndValidateWithOptions(r *http.Request, f FormComponent, options ParseOptions) error {
	if err := ParseWithOptions(r, options); err != nil {
		return err
	}
	if isNilForm(f) {
		return hs_errors.WithCause(ErrInvalidForm, fmt.Errorf("the form must not be nil"))
	}

	if err := f.Bind(r); err != nil {
		var fields FieldErrors
		if errors.As(err, &fields) {
			f.SetFieldErrors(fields)
		}
		return err
	}

	return Validate(f)
}
