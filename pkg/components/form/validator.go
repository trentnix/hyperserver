// validator.go defines a validator that can be used to validate form values
package form

import (
	"regexp"

	"github.com/go-playground/validator/v10"
)

const (
	minimumPasswordLength = 8
)

// Validator provides validation mainly validating structs within the web context
type Validator struct {
	validator *validator.Validate
}

// NewValidator creats a new Validator
func NewValidator() *Validator {
	validate := validator.New()

	// Register the custom validation function
	err := validate.RegisterValidation("password", validatePasswordComplexity)
	if err != nil {
		return nil
	}

	return &Validator{
		validator: validate,
	}
}

// Validate validates the provided form
func (v *Validator) Validate(f any) error {
	if err := v.validator.Struct(f); err != nil {
		return err
	}
	return nil
}

// validatePasswordComplexity is a custom validator that guarantees the provided field meets the
// password complexity standards defined in the password package
func validatePasswordComplexity(field validator.FieldLevel) bool {
	password := field.Field().String()

	if len(password) < minimumPasswordLength {
		return false
	}

	// Check for at least one letter
	hasLetter := regexp.MustCompile(`[A-Za-z]`).MatchString(password)

	// Check for at least one digit
	hasDigit := regexp.MustCompile(`\d`).MatchString(password)

	// Check for at least one special character
	hasSpecialChar := regexp.MustCompile(`[@$!%*?&]`).MatchString(password)

	// Return true only if all conditions are met
	if hasLetter && hasDigit && hasSpecialChar {
		return true
	}

	return false
}
