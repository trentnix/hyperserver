package auth

import (
	"fmt"

	hs_errors "github.com/trentnix/hyperserver/pkg/errors"
)

// BaseError aliases the shared HyperServer error wrapper.
type BaseError = hs_errors.BaseError

// ErrEmailAuthServiceInit indicates an error configuring an instance of EmailAuthService
type ErrEmailAuthServiceInit struct {
	*BaseError
}

// NewErrEmailAuthServiceInit returns an ErrEmailAuthServiceInit wrapping err.
func NewErrEmailAuthServiceInit(err error) *ErrEmailAuthServiceInit {
	return &ErrEmailAuthServiceInit{
		BaseError: &BaseError{
			Err:     err,
			Message: "unable to initialize the email authorization service",
		},
	}
}

// ErrAuthServiceNotFound indicates the AuthService implementation specified is unavailable
type ErrAuthServiceNotFound struct {
	service string
	*BaseError
}

// NewErrAuthServiceNotFound returns an ErrAuthServiceNotFound wrapping err.
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

// NewErrUserRegistration returns an ErrUserRegistration wrapping err.
func NewErrUserRegistration(username string, err error) *ErrUserRegistration {
	errorMessage := fmt.Sprintf("there was an error trying to register the specified user '%s'", username)
	return &ErrUserRegistration{
		BaseError: &BaseError{
			Err:     err,
			Message: errorMessage,
		},
	}
}
