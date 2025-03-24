// auth_service.go defines the AuthService interface that can be used to implement
// an authorization service
package auth

import (
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

// authServices is the array of AuthService instances that is used when an AuthService
// implementation self-registers via init()
var authServices []AuthService

// AuthService defines an interface for an authorization service that can be implemented
// and used in the application
type AuthService interface {
	// defines the custom routes a particular AuthService implementation will handle
	Routes(*http.ServeMux)
	// sets up the AuthService implementation
	Init(*server.ApplicationServer) error
	// determines whether the AuthService in question is configured correctly
	IsLoaded() bool
	// returns the auth type
	AuthType() string

	// returns a template.HTML object so that the AuthService can render a button to access the
	// login capabilities of the AuthService in question
	GetLoginButton() template.HTML
	// returns a template.HTML object so that the AuthService can render a button to access the
	// login capabilities of the AuthService in question
	GetRegisterButton() template.HTML

	// handles the login request for the implemented AuthService
	GetLogin(http.ResponseWriter, *http.Request)
	// handles the login action for the implemented AuthService
	Login(http.ResponseWriter, *http.Request) *user.User

	// handles the registration request for the implemented AuthService
	GetRegister(http.ResponseWriter, *http.Request)
	// handles the registration action for the implemented AuthService
	Register(http.ResponseWriter, *http.Request) bool

	// retrieves the mechanism for a user to request to reset their authorization with the implemented AuthService
	GetResetRequest(http.ResponseWriter, *http.Request)
	// handles a request to reset a user's authorization
	ResetRequest(w http.ResponseWriter, r *http.Request, tokenExpiration time.Duration) bool
	// retrieves the mechanism for a user to reset their authorization with the implemented AuthService
	GetReset(w http.ResponseWriter, r *http.Request, token string)
	// handles the reset authorization action for the implemented AuthService
	Reset(w http.ResponseWriter, r *http.Request, u *user.User, token string, requireNewCredentials bool) bool

	// retrieves the mechanism for a user to change their auth
	GetChange(w http.ResponseWriter, r *http.Request)
	// handles the change auth action
	Change(w http.ResponseWriter, r *http.Request, u *user.User) bool
}

// Register used by a Handler to register itself with the application
func Register(a AuthService) {
	authServices = append(authServices, a)
}

// GetAuthServices retrieves the AuthService instances that have been registered with the application
func GetAuthServices() []AuthService {
	return authServices
}

// RemoveAuthService removes any AuthService implementations from the registered
// AuthServices that matches the specified authType. This is to deal with
// situations where the AuthService could not be initialized or is disabled.
func RemoveAuthService(authType string) []AuthService {
	i := 0
	for _, service := range authServices {
		if service.AuthType() != authType {
			authServices[i] = service
			i++
		}
	}
	// Slice off the removed elements.
	authServices = authServices[:i]
	return authServices
}

// GetLoadedAuthServices retrieves the AuthService instances that have been registered with the application
func GetLoadedAuthServices() []AuthService {
	var loadedAuthServices []AuthService
	for _, s := range authServices {
		if s.IsLoaded() {
			loadedAuthServices = append(loadedAuthServices, s)
		}
	}

	return loadedAuthServices
}

// GetAuthConfigOptions retrieves the configuration options map for the specified
// authType
func GetAuthConfigOptions(c *config.Config, authType string) map[string]string {
	for configOption, configData := range c.Auth.Services {
		if strings.EqualFold(configOption, authType) {
			return configData
		}
	}

	return nil
}
