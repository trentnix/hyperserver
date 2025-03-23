// form.go defines the FormComponent interface and provides an implementation of the interface that
// can be used by structures that represent form data in the application.
package form

import (
	"fmt"
	"html/template"
	"net/http"

	"github.com/go-playground/validator/v10"
)

// Form is an implementation of the FormComponent interface
type Form struct {
	isValidated bool
	formMessage string
	formError   string
	fieldErrors map[string][]string

	ActionUrl string
}

// IsValidated specifies whether the form has been validated (but not whether any errors were found)
func (f *Form) IsValidated() bool {
	return f.isValidated
}

// SetValidated sets the form status to having been validated or not
func (f *Form) SetValidated(v bool) {
	f.isValidated = v
}

// GetFieldErrors gets the errors messages for the specified field
func (f *Form) GetFieldErrors(field string) []string {
	if f.fieldErrors == nil {
		return []string{}
	}
	return f.fieldErrors[field]
}

// GetAllFieldErrors gets all the field error messages in a single slice
func (f *Form) GetAllFieldErrors() []string {
	var allErrors []string
	if f.fieldErrors == nil {
		return allErrors
	}

	for _, errors := range f.fieldErrors {
		allErrors = append(allErrors, errors...)
	}

	return allErrors
}

// SetFieldErrors parses the specified field-level validation errors and adds them to the fieldErrors map
func (f *Form) SetFieldErrors(err error) {
	ves, ok := err.(validator.ValidationErrors)
	if !ok {
		return
	}

	for _, ve := range ves {
		var message string

		// Provide better error messages depending on the failed validation tag
		// This should be expanded as you use additional tags in your validation
		switch ve.Tag() {
		case "required":
			message = "This field is required."
		case "email":
			message = "Enter a valid email address."
		case "password":
			message = "Password values must contain at least 8 characters, one letter, one number, and one special character"
		case "eqfield":
			message = "Passwords do not match."
		case "nefield":
			message = "New passwords must be different from the previous password."
		case "gte":
			message = fmt.Sprintf("Must be greater than or equal to %v.", ve.Param())
		default:
			message = "Invalid value."
		}

		// Add the error
		if f.fieldErrors == nil {
			f.fieldErrors = make(map[string][]string)
		}

		f.fieldErrors[ve.Field()] = append(f.fieldErrors[ve.Field()], message)
	}
}

// HasFieldErrors returns true if the specified field has errors
func (f *Form) HasFieldErrors(field string) bool {
	return len(f.GetFieldErrors(field)) > 0
}

// GetFormError gets a form-level error message
func (f *Form) GetFormError() string {
	return f.formError
}

// SetFormError sets a form-level error message
func (f *Form) SetFormError(e string) {
	f.formError = e
}

// HasErrors returns true if the form has field-level or form-level errors
func (f *Form) HasErrors() bool {
	if len(f.fieldErrors) > 0 {
		return true
	}

	if len(f.formError) > 0 {
		return true
	}

	return false
}

// GetMessage returns a form message to display
func (f *Form) GetFormMessage() string {
	return f.formMessage
}

// SetMessage sets a form message that can be displayed
func (f *Form) SetFormMessage(message string) {
	f.formMessage = message
}

// GetFormMessageHTML returns the HTML version of formMessage
func (f *Form) GetFormMessageHTML() template.HTML {
	return template.HTML(f.formMessage)
}

// GetFormErrorHTML returns the HTML version of formError
func (f *Form) GetFormErrorHTML() template.HTML {
	return template.HTML(f.formError)
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
