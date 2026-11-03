package auth

import (
	"errors"
	"log"
	"net/http"

	"github.com/trentnix/hyperserver/auth/identity"
	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/services/flash"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/trentnix/hyperserver/pkg/util"
)

const authVerifyTemplate = "templates/html/pages/verify.html"

// SendVerificationRequest processes a user verification request by sending verification
// instructions to the user
func (a *AuthManager) SendVerificationRequest(w http.ResponseWriter, r *http.Request) {
	authUser, err := identity.GetAuthenticatedUser(r, a.accounts)
	if err != nil {
		logVerificationFailure(r, "verification account lookup failed")
		a.contentManager.HandleError(w, r, "Unable to send verification instructions. Please try again later.", nil, http.StatusInternalServerError)
		return
	}
	if authUser == nil {
		a.contentManager.HandleError(w, r, "Authentication required", nil, http.StatusUnauthorized)
		return
	}

	if authUser.NeedsVerification() {
		authService := a.getAuthService(authUser.RegistrationAuthType)
		var sender VerificationEmailSender
		if authService != nil {
			sender, _ = authService.(VerificationEmailSender)
		}
		if sender == nil {
			logVerificationFailure(r, "verification email provider is unavailable")
			a.renderVerificationRequest(w, r, true, "Unable to send verification instructions. Please try again later.", http.StatusServiceUnavailable)
			return
		}

		origin, err := util.BuildPublicURL(r, a.httpConfig, "", nil)
		if err == nil {
			err = sender.SendVerificationEmail(r.Context(), authUser, *origin)
		}
		if err != nil {
			// Provider errors can include tokens or message bodies. Do not expose them.
			logVerificationFailure(r, "verification email delivery failed")
			a.renderVerificationRequest(w, r, true, "Unable to send verification instructions. Please try again later.", http.StatusServiceUnavailable)
			return
		}
	}

	a.renderVerificationRequest(w, r, authUser.NeedsVerification(), "Verification email sent. Please check your email for instructions.", http.StatusOK)
}

func logVerificationFailure(r *http.Request, message string) {
	if logger.Get(r.Context()) != nil {
		logger.LogRequestError(r, errors.New(message))
	} else {
		log.Print(message)
	}
}

// GetVerify displays a confirmation form without consuming the verification token.
func (a *AuthManager) GetVerify(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	verificationToken := r.URL.Query().Get("token")
	if verificationToken == "" {
		a.contentManager.HandleError(w, r, "No verification token specified. Unable to verify the user.", nil, http.StatusBadRequest)
		return
	}

	c := content.NewManagedContent(r, a.contentManager)
	c.PartialName = "auth.page.verify"
	c.Templates = a.templates[authVerifyTemplate]
	c.Data = verificationToken
	if err := c.Render(w, r); err != nil {
		c.HandleRenderError(w, r, "Unable to display the verification form.", err)
	}
}

// Verify consumes the submitted token and verifies the account's email address.
func (a *AuthManager) Verify(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	if err := form.Parse(r); err != nil {
		util.HttpError(w, r, err.Error(), nil, err.StatusCode)
		return
	}
	verificationToken := r.PostForm.Get("token")
	if verificationToken == "" {
		a.contentManager.HandleError(w, r, "No verification token specified. Unable to verify the user.", nil, http.StatusBadRequest)
		return
	}

	verificationErrMsg := "Verification failed: unable to verify your user account."

	authenticatedUser, err := identity.GetAuthenticatedUser(r, a.accounts)
	if err != nil {
		logVerificationFailure(r, "verification account lookup failed")
		util.HttpError(w, r, "Unable to verify your account. Please try again later.", nil, http.StatusInternalServerError)
		return
	}

	verifiedUser, err := user.Verify(r.Context(), a.accounts, verificationToken)
	if err != nil {
		logger.Audit(r.Context(), "verification", "failed")
	} else {
		logger.Audit(r.Context(), "verification", "succeeded")
	}
	if err != nil {
		a.recoveryError(w, r, "Verification failed", err)
		return
	}

	if verifiedUser == nil {
		a.contentManager.HandleError(
			w,
			r,
			verificationErrMsg,
			errors.New("verification repository returned no account"),
			http.StatusInternalServerError)
		return
	}

	if authenticatedUser != nil {
		if authenticatedUser.ID == verifiedUser.ID {
			if err := identity.SetAuthenticatedUser(w, r, verifiedUser); err != nil {
				logger.LogRequestError(r, errors.New("verification succeeded but the authenticated session could not be renewed"))
				util.HttpError(w, r, "Your account was verified, but the session could not be renewed. Please log in again.", nil, http.StatusInternalServerError)
				return
			}
		}
		// a user is currently authenticated - redirect to Home with a custom message
		authenticatedUserMessage := "Your account was verified successfully."
		if authenticatedUser.ID != verifiedUser.ID {
			authenticatedUserMessage = "The account was verified successfully, but is not currently logged in. Logout and login with the newly verified account for access."
		}

		err = flash.AddSuccessNotification(w, r, authenticatedUserMessage)
		if err != nil {
			logger.LogRequestError(r, errors.New("verification succeeded but its notification could not be saved"))
		}

		util.RedirectToURL(w, r, a.contentManager.HomeURL)

		return
	}

	// no user is authenticated - redirect the requestor to the login page
	err = flash.AddSuccessNotification(w, r, "Your account was verified successfully. Please login to access the site.")
	if err != nil {
		logger.LogRequestError(r, errors.New("verification succeeded but its notification could not be saved"))
	}

	util.RedirectToURL(w, r, a.contentManager.HomeURL)
}

// GetVerificationRequest displays account verification status and a resend form.
// Signed-in users can access it before verifying their email. GET never sends mail.
func (a *AuthManager) GetVerificationRequest(w http.ResponseWriter, r *http.Request) {
	u, err := identity.GetAuthenticatedUser(r, a.accounts)
	if err != nil {
		logVerificationFailure(r, "verification account lookup failed")
		a.contentManager.HandleError(w, r, "Unable to load your verification status. Please try again later.", nil, http.StatusInternalServerError)
		return
	}
	if u == nil {
		a.contentManager.HandleError(w, r, "Authentication required", nil, http.StatusUnauthorized)
		return
	}

	a.renderVerificationRequest(w, r, u.NeedsVerification(), "", http.StatusOK)
}

func (a *AuthManager) renderVerificationRequest(w http.ResponseWriter, r *http.Request, needsVerification bool, message string, status int) {
	c := content.NewManagedContent(r, a.contentManager)
	c.PartialName = "auth.page.verification.request"
	c.Templates = a.templates["templates/html/pages/verification-request.html"]
	c.Headers = map[string]string{"Content-Type": "text/html; charset=utf-8"}
	c.ResponseStatusCode = status
	c.Data = struct {
		NeedsVerification bool
		Message           string
	}{needsVerification, message}
	if err := c.Render(w, r); err != nil {
		c.HandleRenderError(w, r, "Unable to display verification instructions.", err)
	}
}
