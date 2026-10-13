package auth

import (
	"context"
	"errors"
	"net/http"

	"github.com/trentnix/hyperserver/auth/password"
	"github.com/trentnix/hyperserver/pkg/database"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

// ProcessRegistration hashes credentials and inserts a new account.
// It does not update an existing account or send verification mail. The returned
// message is suitable for the registration form when an error occurs.
func ProcessRegistration(ctx context.Context, accounts user.AccountRepository, uname string, pw string, authType string, verificationRequired bool) (string, error) {
	if err := ctx.Err(); err != nil {
		return "There was an error creating a user account", NewErrUserRegistration(uname, err)
	}
	if accounts == nil {
		return "There was an error creating a user account", NewErrUserRegistration(uname, database.NewErrDatabaseUnavailable(nil))
	}

	var err error
	if pw != "" {
		pw, err = password.HashPassword(pw)
		if err != nil {
			return "There was an error creating a user account", NewErrUserRegistration(uname, err)
		}
	}

	u := &user.User{
		Email:                uname,
		Password:             pw,
		RegistrationAuthType: authType,
		VerificationRequired: verificationRequired,
	}

	// Storage decides which competing registration succeeds.
	err = accounts.Create(ctx, u)
	if err != nil {
		registrationErr := NewErrUserRegistration(uname, err)
		if errors.Is(err, user.ErrEmailAlreadyExists) {
			return "The specified user is already registered", registrationErr
		}
		return "There was an error creating a user account", registrationErr
	}

	return "", nil
}

// RegistrationUnavailable rejects registration requests when registration is disabled.
func RegistrationUnavailable(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Registration is not available.", http.StatusForbidden)
}
