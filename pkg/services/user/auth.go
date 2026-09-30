package user

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/pkg/services/session"
)

const (
	userSessionKey string = "auth-user-session"
)

// SetAuthenticatedUser saves the account ID in the current user session.
// It does not rotate the session ID. Session management must be attached to r.
func SetAuthenticatedUser(w http.ResponseWriter, r *http.Request, u *User) error {
	if u == nil || u.ID == "" {
		return errors.New("The specified user is not specified. The user's ID must be set.")
	}

	s, err := session.Get(r, userSessionKey)
	if err != nil {
		return err
	}

	s.Data[userSessionKey] = u.ID
	sessionSaveErr := s.Save(w, r)
	if sessionSaveErr != nil {
		return sessionSaveErr
	}

	return nil
}

// LogoutAuthenticatedUser delegates logout to the user session's End method.
// It does not clear a User already attached to the request context. Revocation
// of captured cookies depends on the store and is not provided by the current stores.
func LogoutAuthenticatedUser(w http.ResponseWriter, r *http.Request) error {
	s, err := session.Get(r, userSessionKey)
	if err != nil {
		return err
	}

	userId, ok := s.Data[userSessionKey]
	if !ok || userId == "" {
		s.End(w, r)
		return nil
	}

	err = s.End(w, r)
	if err != nil {
		return err
	}

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
	u_db, userRetrievalErr := GetUserByID(db, userId)
	if userRetrievalErr != nil {
		if errors.Is(userRetrievalErr, sql.ErrNoRows) {
			return nil, NewErrUserNotFound(userRetrievalErr)
		}

		return nil, userRetrievalErr
	}

	return u_db, nil
}
