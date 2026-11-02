package auth

import (
	"html/template"
	"net/http"

	"github.com/trentnix/hyperserver/pkg/components/content"
)

const (
	authRegisterSelectionTemplate = "templates/html/pages/register-selection.html"
	authRegisterDefaultTemplate   = "templates/html/pages/register-default.html"
	authRegisterSelectionPartial  = "auth.page.register.selection"
	authRegisterDefaultPartial    = "auth.page.register.default"
)

// GetRegister renders the various authentication options to a user trying to register
// with the application. If there is only a single authentication option, the user is
// sent directly to the registration page of that authentication service.
func (a *AuthManager) GetRegister(w http.ResponseWriter, r *http.Request) {
	if !a.registrationEnabled {
		RegistrationUnavailable(w, r)
		return
	}

	c := content.NewManagedContent(r, a.contentManager)

	var loginHTML []template.HTML

	var authServices []RegistrationHandler
	for _, service := range a.Services.Services() {
		if handler, ok := service.(RegistrationHandler); ok {
			authServices = append(authServices, handler)
		}
	}
	if len(authServices) == 0 {
		RegistrationUnavailable(w, r)
		return
	}
	if len(authServices) == 1 {
		// there is only 1 auth service - display the default
		c.PartialName = authRegisterDefaultPartial
		c.Templates = a.templates[authRegisterDefaultTemplate]
		c.Data = "/auth/register/" + authServices[0].AuthType()
	} else {
		c.PartialName = authRegisterSelectionPartial
		for _, service := range authServices {
			loginHTML = append(loginHTML, service.GetRegisterButton())
		}

		c.Templates = a.templates[authRegisterSelectionTemplate]
		c.Data = loginHTML
	}

	if err := c.Render(w, r); err != nil {
		c.HandleRenderError(w, r, "could not render the registration page", err)
	}
}

// GetRegisterService retrieves the first step of registration process for the given
// AuthService (specified by authType)
func (a *AuthManager) GetRegisterService(w http.ResponseWriter, r *http.Request) {
	if !a.registrationEnabled {
		RegistrationUnavailable(w, r)
		return
	}

	authType := r.PathValue("authType")
	if authType == "" {
		a.contentManager.HandleError(w, r, "No authorization service was specified. Unable to register.", nil, http.StatusInternalServerError)
		return
	}

	authService, ok := a.getAuthService(authType).(RegistrationHandler)
	if !ok {
		http.NotFound(w, r)
		return
	}

	authService.GetRegister(w, r)
}

// Register starts the registration process for the given AuthService (specified by authType)
func (a *AuthManager) Register(w http.ResponseWriter, r *http.Request) {
	if !a.registrationEnabled {
		RegistrationUnavailable(w, r)
		return
	}

	authType := r.PathValue("authType")
	if authType == "" {
		a.contentManager.HandleError(w, r, "No authorization service was specified. Unable to register.", nil, http.StatusInternalServerError)
		return
	}

	authService, ok := a.getAuthService(authType).(RegistrationHandler)
	if !ok {
		http.NotFound(w, r)
		return
	}

	authService.Register(w, r)
}
