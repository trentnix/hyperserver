package auth

import (
	"html/template"
	"net/http"

	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/util"
)

const (
	authRegisterSelectionTemplate = "templates/html/pages/register-selection.html"
	authRegisterSelectionPartial  = "auth.page.register.selection"
	registrationEndpoint          = "/register"
)

// GetRegister composes configured registration methods on one shared page.
func (a *AuthManager) GetRegister(w http.ResponseWriter, r *http.Request) {
	if !a.registrationAvailable(w, r) {
		return
	}
	choices := a.registrationChoices()
	if len(choices) == 0 {
		RegistrationUnavailable(w, r)
		return
	}
	selected := ""
	if values, present := r.URL.Query()["provider"]; present {
		if len(values) != 1 || values[0] == "" {
			util.HttpError(w, r, "Choose a registration method.", nil, http.StatusBadRequest)
			return
		}
		for _, provider := range choices {
			if _, inline := provider.(RegistrationFormHandler); inline && provider.AuthType() == values[0] {
				selected = provider.AuthType()
				break
			}
		}
		if selected == "" {
			http.NotFound(w, r)
			return
		}
	}
	a.renderRegistration(w, r, choices, selected, "")
}

func (a *AuthManager) renderRegistration(w http.ResponseWriter, r *http.Request, choices []RegistrationHandler, selected string, submitted template.HTML) {
	page := ProviderPage{Workflow: "register", Title: "Register", Form: submitted}
	if submitted == "" {
		for _, provider := range choices {
			if selected != "" && provider.AuthType() != selected {
				continue
			}
			if form, ok := provider.(RegistrationFormHandler); ok {
				var err error
				page.Form, err = form.RegistrationForm(r)
				if err != nil {
					a.contentManager.HandleError(w, r, "Unable to display the registration form.", err, http.StatusInternalServerError)
					return
				}
				selected = provider.AuthType()
				break
			}
		}
	}
	for _, provider := range choices {
		if provider.AuthType() != selected {
			page.Controls = append(page.Controls, provider.GetRegisterButton())
		}
	}
	a.renderAuthPage(w, r, page, authRegisterSelectionTemplate, authRegisterSelectionPartial)
}

// GetRegisterService redirects supported legacy provider pages to /register.
func (a *AuthManager) GetRegisterService(w http.ResponseWriter, r *http.Request) {
	if !a.registrationEnabled {
		RegistrationUnavailable(w, r)
		return
	}
	if _, ok := a.getAuthService(r.PathValue("authType")).(RegistrationHandler); !ok {
		http.NotFound(w, r)
		return
	}
	a.redirectRegistration(w, r)
}

func (a *AuthManager) redirectRegistration(w http.ResponseWriter, r *http.Request) {
	util.RedirectToURL(w, r, registrationEndpoint)
}

// Register dispatches a bounded submission. Providers own account creation and
// success responses. Validation failures retain all configured alternatives.
func (a *AuthManager) Register(w http.ResponseWriter, r *http.Request) {
	if !a.registrationAvailable(w, r) {
		return
	}
	if err := form.Parse(r); err != nil {
		util.HttpError(w, r, err.Error(), nil, err.StatusCode)
		return
	}
	values := r.PostForm["provider"]
	if len(values) != 1 || values[0] == "" {
		util.HttpError(w, r, "Choose a registration method.", nil, http.StatusBadRequest)
		return
	}
	selected := values[0]
	provider, ok := a.getAuthService(selected).(RegistrationHandler)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if result := provider.Register(w, r); result.Form != "" {
		a.renderRegistration(w, r, a.registrationChoices(), selected, result.Form)
	}
}

func (a *AuthManager) registrationAvailable(w http.ResponseWriter, r *http.Request) bool {
	if !a.registrationEnabled {
		RegistrationUnavailable(w, r)
		return false
	}
	return a.Policy.RegistrationAvailable(w, r)
}

func (a *AuthManager) registrationChoices() []RegistrationHandler {
	var choices []RegistrationHandler
	for _, service := range a.Services.Services() {
		if provider, ok := service.(RegistrationHandler); ok {
			choices = append(choices, provider)
		}
	}
	return choices
}
