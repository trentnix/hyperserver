// store.go provides a definition for the SessionStore interface and the errors that an
// implementation of the SessionStore interface might use
package session

import (
	"errors"
	"net/http"
)

type (
	SessionStore interface {
		Get(r *http.Request, name string) (*Session, error)
		New(r *http.Request, name string) (*Session, error)
		Save(r *http.Request, w http.ResponseWriter, s *Session) error
		End(r *http.Request, w http.ResponseWriter, s *Session) error
	}
)

var (
	ErrSessionKeyInvalid = errors.New("the session key is invalid")
	ErrInvalidToken      = errors.New("invalid token")
	ErrTokenKeyNotSet    = errors.New("the JWT encryption key was not set")
)
