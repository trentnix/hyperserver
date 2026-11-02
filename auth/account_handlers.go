package auth

import (
	"net/http"

	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/trentnix/hyperserver/pkg/util"
)

const accountTemplate = "templates/html/pages/account.html"

// GetAccount displays account controls supplied by enabled providers. The route
// requires login and any configured verification. Only the email and action
// links reach the template.
func (a *AuthManager) GetAccount(w http.ResponseWriter, r *http.Request) {
	u := user.GetUserFromContext(r.Context())
	if u == nil || u.NeedsVerification() {
		a.contentManager.HandleError(w, r, "Log in with a verified account to manage your login methods.", nil, http.StatusForbidden)
		return
	}
	if !a.Policy.Admit(w, r, u.Email, a.contentManager) {
		return
	}
	var actions []AccountAction
	for _, provider := range a.Services.Services() {
		if controls, ok := provider.(AccountActionsProvider); ok {
			for _, action := range controls.AccountActions(u) {
				if action.Label == "" || util.SafeRedirectURL(action.URL, "") != action.URL || action.URL == "" {
					a.contentManager.HandleError(w, r, "Account controls are temporarily unavailable.", nil, http.StatusInternalServerError)
					return
				}
				actions = append(actions, action)
			}
		}
	}
	c := content.NewManagedContent(r, a.contentManager)
	c.Title, c.PartialName = "Account", "auth.page.account"
	c.Templates = a.templates[accountTemplate]
	c.Data = struct {
		Email   string
		Actions []AccountAction
	}{u.Email, actions}
	if err := c.Render(w, r); err != nil {
		c.HandleRenderError(w, r, "Unable to display your account.", err)
	}
}
