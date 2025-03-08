package auth

import (
	"errors"
	"net/http"

	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/pkg/components/user"
	"github.com/trentnix/hyperserver/pkg/services/session"
)

const (
	userSessionKey string = "auth-user-session"
)

// SetAuthenticatedUser creates a user session for the specified user to indicate that
// the user is authenticated
func SetAuthenticatedUser(r *http.Request, w http.ResponseWriter, u *user.User) error {
	if u == nil || u.ID == "" {
		return errors.New("The specified user is not specified. The user's ID must be set.")
	}

	sessionManager := session.GetSessionManager()
	s, err := sessionManager.Get(r, userSessionKey)
	if err != nil {
		return err
	}

	s.Data[userSessionKey] = u.ID
	sessionSaveErr := s.Save(r, w)
	if sessionSaveErr != nil {
		return sessionSaveErr
	}

	return nil
}

// GetAuthenticatedUser retrieves the currently authenticated user
func GetAuthenticatedUser(r *http.Request, db *sqlx.DB) (*user.User, error) {
	// first check the request context
	hs_user := user.GetUserFromContext(r.Context())
	if hs_user != nil {
		return hs_user, nil
	}

	// check the session
	sessionManager := session.GetSessionManager()
	s, err := sessionManager.Get(r, userSessionKey)
	if err != nil {
		return nil, err
	}

	userId := s.Data[userSessionKey]
	if userId == "" {
		return nil, nil
	}

	// get the user's information from the database
	user, userRetrievalErr := user.GetUserByID(db, userId)
	if userRetrievalErr != nil {
		return nil, userRetrievalErr
	}

	return user, nil
}
