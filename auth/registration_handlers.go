package auth

import (
	"context"
	"html/template"
	"net/http"

	"github.com/trentnix/hyperserver/pkg/components/content"
)

const (
	authRegisterSelectionTemplate = "templates/html/pages/register-selection.html"
	authRegisterSelectionPartial  = "auth.page.register.selection"
)

// GetRegister renders an inline registration form with other enabled providers.
// If no provider offers an inline form, it displays the provider choices.
func (a *AuthManager) GetRegister(w http.ResponseWriter, r *http.Request) {
	if !a.registrationEnabled {
		RegistrationUnavailable(w, r)
		return
	}
	if !a.Policy.RegistrationAvailable(w, r) {
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
		authServices[0].GetRegister(w, r)
		return
	} else {
		for _, provider := range authServices {
			if form, ok := provider.(RegistrationFormHandler); ok {
				form.GetRegisterForm(w, a.withRegistrationAlternatives(r, provider.AuthType()))
				return
			}
		}
		c.PartialName = authRegisterSelectionPartial
		for _, service := range authServices {
			loginHTML = append(loginHTML, service.GetRegisterButton())
		}

		c.Templates = a.templates[authRegisterSelectionTemplate]
		c.Data = ProviderPage{Workflow: "register", Title: "Register", Controls: loginHTML}
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
	if !a.Policy.RegistrationAvailable(w, r) {
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

	authService.GetRegister(w, a.withRegistrationAlternatives(r, authType))
}

// Register starts the registration process for the given AuthService (specified by authType)
func (a *AuthManager) Register(w http.ResponseWriter, r *http.Request) {
	if !a.registrationEnabled {
		RegistrationUnavailable(w, r)
		return
	}
	if !a.Policy.RegistrationAvailable(w, r) {
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

	authService.Register(w, a.withRegistrationAlternatives(r, authType))
}

type registrationAlternativesKey struct{}

// RegistrationAlternatives returns other enabled providers' trusted registration
// controls. The manager attaches them to forms and submissions so validation
// errors preserve the available registration methods.
func RegistrationAlternatives(r *http.Request) []template.HTML {
	buttons, _ := r.Context().Value(registrationAlternativesKey{}).([]template.HTML)
	return buttons
}

func (a *AuthManager) withRegistrationAlternatives(r *http.Request, selected string) *http.Request {
	var buttons []template.HTML
	for _, service := range a.Services.Services() {
		if provider, ok := service.(RegistrationHandler); ok && provider.AuthType() != selected {
			buttons = append(buttons, provider.GetRegisterButton())
		}
	}
	return r.WithContext(context.WithValue(r.Context(), registrationAlternativesKey{}, buttons))
}
