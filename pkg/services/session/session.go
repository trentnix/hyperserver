// session.go wraps session management so that whether a JWT or server-managed session is used is
// abstracted from the caller. Currently, the implementation uses a JWT but this could be abstracted
// so that the SessionManager uses a single interface irrespective of how a session is managed
package session

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
)

type (
	// Session is used to manage a user or usage session
	Session struct {
		ID        string
		Data      map[string]string
		name      string
		store     SessionStore
		IsNew     bool
		ExpiresAt time.Time
	}

	contextKey string
)

var (
	ErrStoreNotFound            = errors.New("session store not configured for the specified session")
	ErrStoreDisabled            = errors.New("the specified session store is disabled")
	ErrSessionNotFound          = errors.New("session not found")
	ErrSessionInvalid           = errors.New("the session is invalid")
	ErrSessionCouldNotBeCreated = errors.New("could not create a new session")
)

const (
	SessionContextKey contextKey = "auth-user"
)

// newSession returns a new session instance using the specified session name
// and specified session store. newSession is intended for use only within the
// session package and preferably by a session store implementation.
func newSession(s SessionStore, name string) *Session {
	sessionID := uuid.New().String()
	session := Session{
		ID:    sessionID,
		name:  name,
		store: s,
		IsNew: true,
	}

	session.Data = make(map[string]string)
	return &session
}

// Save saves the specified session to its corresponding store and writes the
// session to a http cookie
func (s *Session) Save(r *http.Request, w http.ResponseWriter) error {
	if s.store == nil {
		return ErrStoreNotFound
	}

	// save the session to the store
	return s.store.Save(r, w, s)
}

// End terminates the specified session in its corresponding store
func (s *Session) End(r *http.Request, w http.ResponseWriter) error {
	if s.store == nil {
		return ErrStoreNotFound
	}

	return s.store.End(r, w, s)
}
