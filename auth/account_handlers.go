package auth

import (
	"context"
	"errors"
	"net/http"
	"slices"

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
	methods, err := a.AccountMethods(r.Context(), u)
	if err != nil {
		a.contentManager.HandleError(w, r, "Account controls are temporarily unavailable.", err, http.StatusServiceUnavailable)
		return
	}
	c := content.NewManagedContent(r, a.contentManager)
	c.Title, c.PartialName = "Account", "auth.page.account"
	c.Templates = a.templates[accountTemplate]
	c.Data = struct {
		Email   string
		Methods []AccountMethod
	}{u.Email, methods}
	if err := c.Render(w, r); err != nil {
		c.HandleRenderError(w, r, "Unable to display your account.", err)
	}
}

// AccountMethods returns presentation data from enabled providers. Applications
// can omit account navigation when no method offers an action.
func (a *AuthManager) AccountMethods(ctx context.Context, u *user.User) ([]AccountMethod, error) {
	var methods []AccountMethod
	if u == nil || u.NeedsVerification() {
		return methods, nil
	}
	for _, provider := range a.Services.Services() {
		if controls, ok := provider.(AccountProvider); ok {
			method, err := controls.AccountMethod(ctx, u)
			if err != nil {
				return nil, err
			}
			if method.Name == "" {
				return nil, errors.New("account method has no name")
			}
			if method.Connected {
				passwordLogin, err := a.disconnectAlternative(ctx, u, provider)
				if err != nil {
					return nil, err
				}
				if passwordLogin != nil {
					method.Actions = append(slices.Clone(method.Actions), AccountAction{Label: "Disconnect " + method.Name, URL: "/auth/unlink/" + provider.AuthType(), Inline: true})
				}
			}
			for _, action := range method.Actions {
				if action.Label == "" || util.SafeRedirectURL(action.URL, "") != action.URL || action.URL == "" {
					return nil, errors.New("invalid account action")
				}
			}
			methods = append(methods, method)
		}
	}
	// Keep connected methods ahead of enrollment actions.
	slices.SortStableFunc(methods, func(a, b AccountMethod) int {
		if a.Connected == b.Connected {
			return 0
		}
		if a.Connected {
			return -1
		}
		return 1
	})
	return methods, nil
}
