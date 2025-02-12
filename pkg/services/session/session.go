// session.go wraps session management so that whether a JWT or server-managed session is used is
// abstracted from the caller. Currently, the implementation uses a JWT but this could be abstracted
// so that the SessionManager uses a single interface irrespective of how a session is managed
package session

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/trentnix/hyperserver/config"
)

type (
	// SessionManager is used to manage sessions
	SessionManager struct {
		config *config.Config

		// Stores is a map of sessoin names to the type of store it should use
		Stores map[string]map[string]string
		Types  map[string]string
	}

	// Session is used to manage a user or usage session
	Session struct {
		ID    string
		Data  map[string]string
		name  string
		store SessionStore
	}

	contextKey string
)

var (
	ErrStoreNotFound   = errors.New("session store not configured for the specified session")
	ErrStoreDisabled   = errors.New("the specified session store is disabled")
	ErrSessionNotFound = errors.New("session not found")
	ErrSessionInvalid  = errors.New("the session is invalid")
)

const (
	SessionContextKey contextKey = "auth-user"
)

// NewSessionManager creates a new instance of a SessionManager object with default values for
// TokenLifetime and cookie name
func NewSessionManager(c *config.Config) *SessionManager {
	sm := SessionManager{
		config: c,
	}

	sm.Stores = c.HTTP.Session.Stores
	sm.Types = c.HTTP.Session.Types

	return &sm
}

// Get returns any current sessions specified in the request with the specified name.
// If a session is not found, a new session will be returned.
func (m *SessionManager) Get(r *http.Request, name string) (*Session, error) {
	store, err := m.getStore(name)
	if err != nil {
		return nil, err
	}

	// retrieve the session from the request
	session, _ := store.Get(r, name)
	if session == nil {
		// if the request doesn't have a session, return a new session
		return m.New(r, name)
	}

	return session, nil
}

// New returns a new Session instance irrespective of whether one already exists with the
// specified name
func (m *SessionManager) New(r *http.Request, name string) (*Session, error) {
	store, err := m.getStore(name)
	if err != nil {
		return nil, err
	}

	// create a new session instance and return
	return newSession(store, name), nil
}

// getStore retrieves the session store that has been configured in the
// application. When a store is implemented this is where the SessionManager
// will retrieve the specified store. The name provided is a session name.
//
// This means session names will need to be registered against a session store type
// name. By allowing session names to be registered against a session store type,
// multiple store types can be used concurrently and the session name can determine
// which session store to use. For example, a visitor session might use cookie
// storage but authenticated users might use filesystem storage or database storage.
func (m *SessionManager) getStore(name string) (SessionStore, error) {
	storeType, ok := m.Types[name]
	if !ok {
		return nil, ErrStoreNotFound
	}

	storeType = strings.ToLower(storeType)

	store, ok := m.Stores[storeType]
	if !ok {
		return nil, ErrStoreNotFound
	}

	if strings.ToLower(store["enabled"]) != "true" && strings.ToLower(store["enabled"]) != "1" {
		return nil, ErrStoreDisabled
	}

	switch strings.ToLower(storeType) {
	case strings.ToLower(cookieStoreName):
		store, err := NewCookieStore(m.config)
		if err != nil {
			return nil, errors.Join(ErrCookieStoreNotCreated, err)
		}

		return store, nil
	}

	return nil, ErrStoreNotFound
}

// newSession returns a new session instance using the specified session name
// and specified session store. newSession is intended for use only within the
// session package and preferably by a session store implementation.
func newSession(s SessionStore, name string) *Session {
	sessionID := uuid.New().String()
	session := Session{
		ID:    sessionID,
		name:  name,
		store: s,
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
