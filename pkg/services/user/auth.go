package user

import (
	"errors"
	"net/http"

	"github.com/trentnix/hyperserver/pkg/services/session"
)

const (
	userSessionKey        = "auth-user-session"
	userSessionVersionKey = "auth-session-version"
)

// ClearAuthenticationCookie removes the browser's authentication cookie without
// loading or revoking its session. Use it to discard an invalid cookie.
func ClearAuthenticationCookie(w http.ResponseWriter, r *http.Request) {
	session.ExpireCookie(w, r, userSessionKey)
}

// SetAuthenticatedUser rotates the session and records the account's session version.
// Call after authenticating credentials or granting new privileges. The account
// must be freshly loaded or saved. Session management must be attached to r.
func SetAuthenticatedUser(w http.ResponseWriter, r *http.Request, u *User) error {
	if u == nil || u.ID == "" {
		return errors.New("The specified user is not specified. The user's ID must be set.")
	}
	if u.SessionVersion < 1 {
		return errors.New("the user's session version must be loaded from account storage")
	}

	s, err := session.Get(r, userSessionKey)
	if err != nil {
		return err
	}

	if err := s.Rotate(w, r, map[string]any{userSessionKey: u.ID, userSessionVersionKey: u.SessionVersion}); err != nil {
		return err
	}
	*r = *AddUserToRequestContext(r, u)
	return nil
}

// LogoutAuthenticatedUser ends the user session and clears the request's user.
// SQLite revokes stored sessions. Cookie-only stores cannot revoke captured cookies.
func LogoutAuthenticatedUser(w http.ResponseWriter, r *http.Request) error {
	s, err := session.Get(r, userSessionKey)
	if err != nil {
		return err
	}

	err = s.End(w, r)
	if err != nil {
		return err
	}
	*r = *r.WithContext(ClearUserFromContext(r.Context()))
	return nil
}

// GetAuthenticatedUser loads the session's account through accounts, unless the
// request already has an authenticated user. Account reads use the request context.
func GetAuthenticatedUser(r *http.Request, accounts AccountRepository) (*User, error) {
	// first check the request context
	u := GetUserFromContext(r.Context())
	if u != nil {
		return u, nil
	}

	s, err := session.Get(r, userSessionKey)
	if err != nil {
		return nil, err
	}

	userId, ok := s.Data[userSessionKey].(string)
	if !ok {
		return nil, nil
	}

	if accounts == nil {
		return nil, errors.New("account repository is not configured")
	}
	u_db, userRetrievalErr := accounts.GetByID(r.Context(), userId)
	if userRetrievalErr != nil {
		return nil, userRetrievalErr
	}

	var version int64
	if err := s.DecodeValue(userSessionVersionKey, &version); err != nil || version != u_db.SessionVersion {
		return nil, nil
	}
	return u_db, nil
}
