// auth_service.go defines the AuthService interface that can be used to implement
// an authorization service
package auth

import (
	"html/template"
	"net/http"

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
}

// Register used by a Handler to register itself with the application
func Register(a AuthService) {
	authServices = append(authServices, a)
}

// GetAuthServices retrieves the AuthService instances that have been registered with the application
func GetAuthServices() []AuthService {
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
