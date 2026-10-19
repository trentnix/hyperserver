package auth

import (
	"net/http"

	"github.com/trentnix/hyperserver/auth/identity"
	"github.com/trentnix/hyperserver/pkg/components/content"
)

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
