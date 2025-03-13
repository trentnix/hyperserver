// session.go defines the Session struct to store session data.
package session

import (
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

const (
	SessionContextKey contextKey = "auth-user"
)

// newSession returns a new Session instance with the specified session name
// serialized to the specified session store. newSession is intended for use
// only within the session package by a session store implementation.
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
// session to a http cookie.
func (s *Session) Save(r *http.Request, w http.ResponseWriter) error {
	if s.store == nil {
		return NewErrSessionStoreNotFound(nil)
	}

	// save the session to the store
	return s.store.Save(r, w, s)
}

// End terminates the specified session in its corresponding store
func (s *Session) End(r *http.Request, w http.ResponseWriter) error {
	if s.store == nil {
		return NewErrSessionStoreNotFound(nil)
	}

	return s.store.End(r, w, s)
}
