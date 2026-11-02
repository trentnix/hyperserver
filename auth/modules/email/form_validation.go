package auth

import (
	"github.com/trentnix/hyperserver/auth/password"
	"github.com/trentnix/hyperserver/pkg/components/form"
)

func validateEmail(email string) form.FieldErrors {
	errors := make(form.FieldErrors)
	if email == "" {
		errors["Email"] = []string{form.RequiredMessage}
	} else if !form.ValidEmail(email) {
		errors["Email"] = []string{form.EmailMessage}
	}
	return errors
}

func validatePasswords(errors form.FieldErrors, field, matchField, value, match string) {
	if message := passwordError(value); message != "" {
		errors[field] = []string{message}
	}
	if message := passwordError(match); message != "" {
		errors[matchField] = []string{message}
	} else if match != value {
		errors[matchField] = []string{"Passwords do not match."}
	}
}

func passwordError(value string) string {
	if value == "" {
		return form.RequiredMessage
	}
	if err := password.ValidatePasswordComplexity(value); err != nil {
		return err.Error()
	}
	return ""
}
