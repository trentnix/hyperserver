package user

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/jmoiron/sqlx"
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

// GetAuthenticatedUser retrieves the currently authenticated user
func GetAuthenticatedUser(r *http.Request, db *sqlx.DB) (*User, error) {
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

	// get the user's information from the database
	u_db, userRetrievalErr := GetUserByIDContext(r.Context(), db, userId)
	if userRetrievalErr != nil {
		if errors.Is(userRetrievalErr, sql.ErrNoRows) {
			return nil, NewErrUserNotFound(userRetrievalErr)
		}

		return nil, userRetrievalErr
	}

	version, ok := s.Data[userSessionVersionKey].(int64)
	if !ok || version != u_db.SessionVersion {
		return nil, nil
	}
	return u_db, nil
}
