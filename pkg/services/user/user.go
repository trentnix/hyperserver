// Package user defines accounts, account tokens, and their persistence contract.
// Applications supply an initialized AccountRepository for persistence.
// Password resets and email verification consume their exact stored token
// atomically with the account change.
package user

import (
	"context"
	"net/http"
	"time"
)

type (
	// User is a persisted account, including its authentication provider and verification state.
	// Password contains the provider's stored password hash, not plaintext.
	User struct {
		ID                   string    `db:"id"`
		Email                string    `db:"email"`
		Verified             bool      `db:"verified"`
		VerificationRequired bool      `db:"verification_required"`
		CreatedAt            time.Time `db:"created_at"`
		UpdatedAt            time.Time `db:"updated_at"`
		RegistrationAuthType string    `db:"registration_auth_type"`
		Password             string    `db:"password"`
		// SessionVersion changes when credentials or account access changes.
		// Callers must preserve the version supplied by account reads and writes.
		SessionVersion int64 `db:"session_version"`
	}

	contextKey string
)

const (
	// UserContextKey identifies the current User stored in a request context.
	UserContextKey contextKey = "auth-user"
)

// NeedsVerification determines whether the specified user is not verified and needs to be
// verified to access resources available to authenticated, verified users
func (user *User) NeedsVerification() bool {
	if !user.VerificationRequired || user.Verified {
		return false
	}

	return true
}

// SetVerified sets the specified user's verification flag. The user will still need to be
// saved to serialize the change to the data store.
func (user *User) SetVerified() {
	user.Verified = true
}

// AddUserToRequestContext adds the specified user to the provided request
func AddUserToRequestContext(r *http.Request, user *User) *http.Request {
	ctx := r.Context()
	ctx = context.WithValue(ctx, UserContextKey, user)
	return r.WithContext(ctx)
}

// GetUserFromContext returns the User attached to ctx, or nil if none is present.
func GetUserFromContext(ctx context.Context) *User {
	if ctx == nil {
		return nil
	}

	user, ok := ctx.Value(UserContextKey).(*User)
	if !ok || user == nil {
		return nil
	}

	return user
}

// ClearUserFromContext deletes any user instance stored in the provided
// context and returns the updated context
func ClearUserFromContext(ctx context.Context) context.Context {
	// Set the user value to nil to indicate clearing the user
	return context.WithValue(ctx, UserContextKey, nil)
}
