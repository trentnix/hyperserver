package user

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/pkg/services/session"
)

const (
	userSessionKey string = "auth-user-session"
)

// SetAuthenticatedUser creates a user session for the specified user to indicate that
// the user is authenticated
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

// LogoutAuthenticatedUser retrieves any active user session and ends the session,
// terminating any logged-in user's authentication session
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

// GetVerificationToken retrieves a new verification token for the specified user
func NewVerificationToken(userId string, jwtKey []byte, expiration time.Duration) (string, error) {
	if len(jwtKey) == 0 {
		return "", NewErrJwtKeyNotSet(fmt.Errorf("unable to send verification token"))
	}

	if userId == "" {
		return "", NewErrUserNotSpecified(fmt.Errorf("a user must be specified to know the verification destination"))
	}

	expirationTime := time.Now().Add(expiration)
	claims := &VerificationClaims{
		Id: userId,
		StandardClaims: jwt.StandardClaims{
			ExpiresAt: expirationTime.Unix(),
		},
	}

	// Create and sign the token with the specified algorithm and claims
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	verificationToken, err := token.SignedString(jwtKey)
	if err != nil {
		return "", NewErrToken(err)
	}

	return verificationToken, nil
}
