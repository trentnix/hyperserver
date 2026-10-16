package user

import "errors"

var (
	// ErrEmailAlreadyExists reports a duplicate account email.
	ErrEmailAlreadyExists = errors.New("an account with that email already exists")
	// ErrUserNotSpecified indicates a user.User instance was required but was not set
	ErrUserNotSpecified = errors.New("a user account was not specified")
	// ErrUserNotFound indicates a user.User instance could not be retrieved
	ErrUserNotFound = errors.New("unable to retrieve the specified user account")
	// ErrUserChanged indicates that an update used an outdated account security version.
	ErrUserChanged = errors.New("account security state changed since it was loaded")
	// ErrToken indicates an error creating and managing a token
	ErrToken = errors.New("token error")
	// ErrInvalidResetToken reports a rejected password-reset token.
	ErrInvalidResetToken = errors.New("the reset token is invalid")
	// ErrTokenNotSpecified indicates the token was not specified
	ErrTokenNotSpecified = errors.New("token not specified")
	// ErrTokenNotFound indicates the token could not be retrieved
	ErrTokenNotFound = errors.New("token not found")
	// ErrTokenExpired indicates that an account token has expired.
	ErrTokenExpired = errors.New("token expired")
	// ErrTokenExpirationNotSpecified indicates no expiration value has been provided
	ErrTokenExpirationNotSpecified = errors.New("token expiration not set")
	// ErrJwtKeyNotSet indicates no JWT key was set
	ErrJwtKeyNotSet = errors.New("JWT key is not set")
)

// IsTokenRejection distinguishes invalid authorization from storage and configuration failures.
// Providers must not use rejection errors for I/O failures.
func IsTokenRejection(err error) bool {
	return errors.Is(err, ErrToken) || errors.Is(err, ErrInvalidResetToken) ||
		errors.Is(err, ErrTokenNotSpecified) || errors.Is(err, ErrTokenNotFound) ||
		errors.Is(err, ErrTokenExpired) || errors.Is(err, ErrUserNotFound) ||
		errors.Is(err, ErrUserChanged)
}
