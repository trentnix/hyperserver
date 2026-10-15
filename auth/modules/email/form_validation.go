package auth

import (
	"github.com/trentnix/hyperserver/auth/password"
	"github.com/trentnix/hyperserver/pkg/components/form"
)

// Validate checks the login email. Existing passwords need not meet creation rules.
func (lf *LoginForm) Validate() form.FieldErrors {
	return validateEmail(lf.Email)
}

// Validate checks the registration email and matching passwords.
func (rf *RegisterForm) Validate() form.FieldErrors {
	errors := validateEmail(rf.Email)
	validatePasswords(errors, "Password", "PasswordMatch", rf.Password, rf.PasswordMatch)
	return errors
}

// Validate checks the account address without looking up the account.
func (rprf *ResetPasswordRequestForm) Validate() form.FieldErrors {
	return validateEmail(rprf.Email)
}

// Validate checks the replacement password and confirmation.
func (rpf *ResetPasswordForm) Validate() form.FieldErrors {
	errors := make(form.FieldErrors)
	validatePasswords(errors, "Password", "PasswordMatch", rpf.Password, rpf.PasswordMatch)
	return errors
}

// Validate requires the current password and a different, matching replacement.
func (cpf *ChangePasswordForm) Validate() form.FieldErrors {
	errors := make(form.FieldErrors)
	if cpf.OldPassword == "" {
		errors["OldPassword"] = []string{form.RequiredMessage}
	}
	validatePasswords(errors, "NewPassword", "NewPasswordMatch", cpf.NewPassword, cpf.NewPasswordMatch)
	if len(errors["NewPassword"]) == 0 && cpf.NewPassword == cpf.OldPassword {
		errors["NewPassword"] = []string{"New passwords must be different from the previous password."}
	}
	return errors
}

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
