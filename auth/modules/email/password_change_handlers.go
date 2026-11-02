package auth

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/trentnix/hyperserver/auth/identity"
	"github.com/trentnix/hyperserver/pkg/components/form"
	hs_errors "github.com/trentnix/hyperserver/pkg/errors"
	"github.com/trentnix/hyperserver/pkg/services/flash"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

const (
	emailChangePasswordFormTemplate = "templates/html/partials/change-password.html"
	emailChangePasswordFormPartial  = "auth.partial.email.change.password.form"
	emailChangePath                 = "/auth/change/email"
)

// ChangePasswordForm binds the current password and matching replacement passwords.
type ChangePasswordForm struct {
	OldPassword      string
	NewPassword      string
	NewPasswordMatch string

	form.Form
}

// Bind populates the ChangePasswordForm fields from the request
func (cpf *ChangePasswordForm) Bind(r *http.Request) error {
	cpf.OldPassword = r.FormValue("oldPassword")
	cpf.NewPassword = r.FormValue("newPassword")
	cpf.NewPasswordMatch = r.FormValue("newPasswordMatch")
	return nil
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

// GetChange serves the password change page with the changePasswordForm form
func (a *EmailAuthService) GetChange(w http.ResponseWriter, r *http.Request) {
	changeErrMsg := "Unable to display the email authorization service password change form"

	changeForm := &ChangePasswordForm{}
	changeForm.ActionUrl = emailChangePath

	change := a.views[emailChangePasswordFormPartial].Content(r, changeForm)

	authMessages, err := flash.GetMessages(w, r, flash.AuthMessages)
	if err != nil {
		a.contentManager.HandleError(w, r, "Unable to load authentication messages", err, http.StatusInternalServerError)
		return
	}
	changeForm.SetMessages(authMessages)

	err = change.Render(w, r)
	if err != nil {
		change.HandleRenderError(w, r, changeErrMsg, err)
		return
	}
}

// Change authenticates the specified user and, if authentication succeeds and a new
// password meets the password complexity criteria, changes the user's password to a new value.
func (a *EmailAuthService) Change(w http.ResponseWriter, r *http.Request, u *user.User) {
	changeErrMsg := "unable to change your password"

	changeForm := &ChangePasswordForm{}
	changeForm.ActionUrl = emailChangePath
	change := a.views[emailChangePasswordFormPartial].Content(r, changeForm)

	if u == nil {
		form.HandleFormError(
			w,
			r,
			change,
			changeForm,
			"Unable to change your password due to an internal error",
			hs_errors.WithCause(user.ErrUserNotSpecified, fmt.Errorf("change failed")))
		return
	}

	err := form.ParseAndValidate(r, changeForm)
	if err != nil {
		form.HandleFormError(w, r, change, changeForm, "Unable to process form data", err)
		return
	}

	err = a.changePassword(r.Context(), u, changeForm.OldPassword, changeForm.NewPassword)
	if err != nil {
		logger.Audit(r.Context(), "password_change", "failed")
	} else {
		logger.Audit(r.Context(), "password_change", "succeeded")
	}
	if errors.Is(err, errInvalidCredentials) {
		form.HandleFormError(w, r, change, changeForm, "The provided password is incorrect", nil)
		return
	}
	if err != nil {
		form.HandleFormError(w, r, change, changeForm, changeErrMsg, err)
		return
	}

	changeForm.OldPassword = ""
	changeForm.NewPassword = ""
	changeForm.NewPasswordMatch = ""
	*r = *r.WithContext(user.ClearUserFromContext(r.Context()))
	identity.ClearAuthenticationCookie(w, r)
	changeForm.AddSuccessMessage("Your password has been changed. Please log in again.")

	change = a.views[emailChangePasswordFormPartial].Content(r, changeForm)
	err = change.Render(w, r)
	if err != nil {
		change.HandleRenderError(
			w,
			r,
			"The password was updated, but there was an error displaying the change form.",
			err)
	}
}
