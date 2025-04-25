// errors.go defines the custom errors used by the auth pkg
package auth

import (
	"fmt"

	hs_errors "github.com/trentnix/hyperserver/pkg/errors"
)

type BaseError = hs_errors.BaseError

// ErrEmailAuthServiceInit indicates an error configuring an instance of EmailAuthService
type ErrEmailAuthServiceInit struct {
	*BaseError
}

// NewErrEmailAuthServiceInit creates an instance of ErrEmailAuthServiceInit
func NewErrEmailAuthServiceInit(err error) *ErrEmailAuthServiceInit {
	return &ErrEmailAuthServiceInit{
		BaseError: &BaseError{
			Err:     err,
			Message: "unable to initialize the email authorization service",
		},
	}
}

// ErrDatabaseUnavailable indicates the database is unavailable
type ErrLoadingTemplate struct {
	template string
	*BaseError
}

// NewErrDatabaseUnavailable creates an instance of ErrDatabaseUnavailable
func NewErrLoadingTemplate(err error, t string) *ErrLoadingTemplate {
	errorMessage := fmt.Sprintf("there was an error loading the specified template: %s", t)
	return &ErrLoadingTemplate{
		template: t,
		BaseError: &BaseError{
			Err:     err,
			Message: errorMessage,
		},
	}
}

// ErrAuthServiceNotFound indicates the AuthService implementation specified is unavailable
type ErrAuthServiceNotFound struct {
	service string
	*BaseError
}

// NewErrDatabaseUnavailable creates an instance of ErrDatabaseUnavailable
func NewErrAuthServiceNotFound(err error, serviceName string) *ErrAuthServiceNotFound {
	errorMessage := fmt.Sprintf("there was an error loading the specified auth service: %s", serviceName)
	return &ErrAuthServiceNotFound{
		service: serviceName,
		BaseError: &BaseError{
			Err:     err,
			Message: errorMessage,
		},
	}
}

// ErrUserRegistration indicates an error during the user registration process
type ErrUserRegistration struct {
	*BaseError
}

// NewErrUserRegistration creates an instance of ErrUserRegistration
func NewErrUserRegistration(username string, err error) *ErrUserRegistration {
	errorMessage := fmt.Sprintf("there was an error trying to register the specified user '%s'", username)
	return &ErrUserRegistration{
		BaseError: &BaseError{
			Err:     err,
			Message: errorMessage,
		},
	}
}
