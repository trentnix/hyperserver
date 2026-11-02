package auth

import (
	"errors"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/services/flash"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/trentnix/hyperserver/pkg/util"
)

const (
	emailResetRequestFormTemplate  = "templates/html/partials/reset-request.html"
	emailResetPasswordFormTemplate = "templates/html/partials/reset-password.html"
	emailResetRequestFormPartial   = "auth.partial.email.reset.request.form"
	emailResetPasswordFormPartial  = "auth.partial.email.reset.password.form"
	emailResetRequestPath          = "/auth/reset/request/email"
	emailResetPath                 = "/auth/reset/email"
)

// ResetPasswordRequestForm defines the fields used when requesting to reset a user's password
type ResetPasswordRequestForm struct {
	Email string

	form.Form
}

// ResetPasswordForm defines the fields used when resetting the password
type ResetPasswordForm struct {
	Password      string
	PasswordMatch string

	form.Form
}

// Bind populates the reset-request email address from r.
func (rprf *ResetPasswordRequestForm) Bind(r *http.Request) error {
	rprf.Email = r.FormValue("email")
	return nil
}

// Bind populates the replacement password and confirmation from r.
func (rpf *ResetPasswordForm) Bind(r *http.Request) error {
	rpf.Password = r.FormValue("password")
	rpf.PasswordMatch = r.FormValue("passwordMatch")
	return nil
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

// GetResetRequest serves the password reset request page with the resetPasswordRequestForm form
func (a *EmailAuthService) GetResetRequest(w http.ResponseWriter, r *http.Request) {
	resetRequestForm := &ResetPasswordRequestForm{}
	resetRequestForm.ActionUrl = emailResetRequestPath
	reset := a.views[emailResetRequestFormPartial].Content(r, resetRequestForm)

	authMessages, err := flash.GetMessages(w, r, flash.AuthMessages)
	if err != nil {
		a.contentManager.HandleError(w, r, "Unable to load authentication messages", err, http.StatusInternalServerError)
		return
	}
	resetRequestForm.SetMessages(authMessages)

	err = reset.Render(w, r)
	if err != nil {
		reset.HandleRenderError(w, r,
			"Unable to display the email authorization service password reset request form",
			err)
		return
	}
}

// ResetRequest uses the same acknowledgment for eligible, unknown, and failed requests.
// The acknowledgment confirms receipt of the request, not delivery of an email.
// Valid requests have a minimum response time, not an additional processing timeout.
// Delivery and rendering errors do not change the completed storage operations.
func (a *EmailAuthService) ResetRequest(w http.ResponseWriter, r *http.Request, tokenExpiration time.Duration) {
	respondAfter := time.Now().Add(a.config.Auth.ResetMinimumResponseTime)
	resetRequestForm := &ResetPasswordRequestForm{}
	resetRequestForm.ActionUrl = emailResetRequestPath
	reset := a.views[emailResetRequestFormPartial].Content(r, resetRequestForm)

	err := form.ParseAndValidate(r, resetRequestForm)
	if err != nil {
		form.HandleFormError(w, r, reset, resetRequestForm, "Unable to process form data", err)
		return
	}

	link, err := util.BuildPublicURL(r, a.config.HTTP, emailResetPath, nil)
	if err == nil {
		err = a.requestPasswordReset(r.Context(), resetRequestForm.Email, tokenExpiration, *link)
	}
	if err != nil {
		// Provider errors can contain the token or email body. Log only the failure.
		if l := logger.Get(r.Context()); l != nil {
			l.Error("password reset instructions could not be sent")
		} else {
			log.Print("password reset instructions could not be sent")
		}
	}

	if err := waitForResetResponse(r.Context(), respondAfter); err != nil {
		return
	}

	resetRequestForm.Email = ""
	resetRequestForm.AddMessage("Password reset request received. If the account is eligible, check your email for instructions. If no email arrives, try again later.")
	reset.Data = resetRequestForm
	err = reset.Render(w, r)
	if err != nil {
		reset.HandleRenderError(
			w,
			r,
			"Unable to display the email authorization service reset request form",
			err)
	}
}

// GetReset serves the password reset page with the resetPasswordForm form
func (a *EmailAuthService) GetReset(w http.ResponseWriter, r *http.Request, token string) {
	resetErrMsg := "Unable to display the email authorization service password reset form"

	if token == "" {
		a.contentManager.HandleError(w, r, "No reset token specified. Please request a new link.", nil, http.StatusBadRequest)
		return
	}

	resetForm := &ResetPasswordForm{}
	resetForm.ActionUrl = emailResetPath + "?" + url.Values{"token": {token}}.Encode()

	reset := a.views[emailResetPasswordFormPartial].Content(r, resetForm)

	authMessages, err := flash.GetMessages(w, r, flash.AuthMessages)
	if err != nil {
		a.contentManager.HandleError(w, r, "Unable to load authentication messages", err, http.StatusInternalServerError)
		return
	}
	resetForm.SetMessages(authMessages)

	err = reset.Render(w, r)
	if err != nil {
		reset.HandleRenderError(w, r, resetErrMsg, err)
		return
	}
}

// Reset validates the password form and atomically redeems the reset token.
func (a *EmailAuthService) Reset(
	w http.ResponseWriter,
	r *http.Request,
	u *user.User,
	token string,
	requireNewCredentials bool,
) {
	resetForm := &ResetPasswordForm{}
	reset := a.views[emailResetPasswordFormPartial].Content(r, resetForm)

	if u == nil {
		a.contentManager.HandleError(w, r, "Unable to reset password. Please try again later.",
			errors.New("password reset requires an account"), http.StatusInternalServerError)
		return
	}

	resetForm.ActionUrl = emailResetPath + "?" + url.Values{"token": {token}}.Encode()

	err := form.ParseAndValidate(r, resetForm)
	if err != nil {
		form.HandleFormError(w, r, reset, resetForm, "Unable to process form data", err)
		return
	}

	err = a.resetPassword(r.Context(), u, token, resetForm.Password, requireNewCredentials)
	if err != nil {
		logger.Audit(r.Context(), "password_reset", "failed")
	} else {
		logger.Audit(r.Context(), "password_reset", "succeeded")
	}
	if errors.Is(err, errPasswordReused) {
		form.HandleFormError(w, r, reset, resetForm,
			"The provided password has been recently used. For security reasons, a new password is required.", nil)
		return
	}
	if err != nil {
		if user.IsTokenRejection(err) {
			a.contentManager.HandleError(w, r,
				"Unable to reset password: the link is invalid or expired. Please request a new link.", nil, http.StatusBadRequest)
			return
		}
		a.contentManager.HandleError(w, r, "Unable to reset password. Please try again later.",
			errors.New("password reset operation failed"), http.StatusInternalServerError)
		return
	}

	*r = *r.WithContext(user.ClearUserFromContext(r.Context()))
	if err := flash.AddSuccessNotification(w, r, "Your password has been updated. Login to access the site."); err != nil {
		logger.LogRequestError(r, errors.New("password reset succeeded but its notification could not be saved"))
	}
	util.RedirectToURL(w, r, a.contentManager.AuthURL)
}
