// auth_service.go defines the AuthService interface that can be used to implement
// an authorization service
package auth

import (
	"html/template"
	"net/http"
	"os"

	"github.com/trentnix/hyperserver/pkg/components/hs_user"
)

// authServices is the array of AuthService instances that is used when an AuthService
// implementation self-registers via init()
var authServices []AuthService

const (
	defaultAuthRedirect = "/"
	AuthTypeGoogle      = "google"
	AuthTypeEmail       = "email"
)

// AuthService defines an interface for an authorization service that can be implemented
// and used in the application
type AuthService interface {
	// defines the custom routes a particular AuthService implementation will handle
	Routes(*http.ServeMux)
	// sets up the AuthService implementation
	// Init(*services.Container) error
	// determines whether the AuthService in question is configured correctly
	IsLoaded() bool
	// returns the auth type
	AuthType() string

	// returns a template.HTML object so that the AuthService can render a button to access the
	// login capabilities of the AuthService in question
	GetLoginButton() template.HTML

	// returns a template.HTML object so that the AuthService can render a button to access the
	// registration capabilities of the AuthService in question
	GetRegisterButton() template.HTML

	// handles the login request for the implemented AuthService
	GetLogin(http.ResponseWriter, *http.Request)
	// handles the registration request for the implemented AuthService
	GetRegister(http.ResponseWriter, *http.Request)

	// handles the login action for the implemented AuthService
	Login(http.ResponseWriter, *http.Request) bool
	// handles the register action for the implemented AuthService
	Register(http.ResponseWriter, *http.Request) bool

	// retrieves the mechanism for a user to request to reset their authorization with the implemented AuthService
	GetResetRequest(http.ResponseWriter, *http.Request)
	// handles a request to reset a user's authorization
	ResetRequest(http.ResponseWriter, *http.Request) bool
	// retrieves the mechanism for a user to reset their authorization with the implemented AuthService
	GetReset(w http.ResponseWriter, r *http.Request, token string)
	// handles the reset authorization action for the implemented AuthService
	Reset(w http.ResponseWriter, r *http.Request, user *hs_user.User, token string) bool

	// retrieves the mechanism for a user to change their auth
	GetChange(w http.ResponseWriter, r *http.Request)
	// handles the change auth action
	Change(w http.ResponseWriter, r *http.Request, user *hs_user.User) bool
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

// LoadHTMLFromFile takes the file at the specified filePath and returns a template.HTML object with
// its contents
func LoadHTMLFromFile(filePath string) (template.HTML, error) {
	htmlBytes, err := os.ReadFile(filePath)
	if err != nil {
		return "", NewErrLoadingTemplate(err, filePath)
	}

	return template.HTML(htmlBytes), nil
}
