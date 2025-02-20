// session.go wraps session management so that whether a JWT or server-managed session is used is
// abstracted from the caller. Currently, the implementation uses a JWT but this could be abstracted
// so that the SessionManager uses a single interface irrespective of how a session is managed
package session

import (
	"context"
	"errors"
	"net/http"
	"strings"

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

	ctxKey int
)

const (
	sessionRegistryKey ctxKey = iota
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
	var session *Session

	session = getCachedSession(r, name)
	if session != nil {
		return session, nil
	}

	store, err := m.getStore(name)
	if err != nil {
		return nil, err
	}

	// retrieve the session from the request
	session, _ = store.Get(r, name)
	if session == nil {
		// if the request doesn't have a session, return a new session
		session, err = m.New(r, name)
		if err != nil {
			return nil, err
		}

		if session != nil {
			setCachedSession(r, session)
			return session, nil
		}

		return nil, ErrSessionCouldNotBeCreated
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

	session := newSession(store, name)
	setCachedSession(r, session)

	// create a new session instance and return
	return session, nil
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

	var store SessionStore
	var storeErr error

	switch {
	case strings.EqualFold(storeType, cookieStoreName):
		store, storeErr = NewCookieStore(m.config)
	case strings.EqualFold(storeType, sqliteStoreName):
		store, storeErr = NewSQLiteStore(m.config)
	}

	if storeErr != nil {
		return nil, errors.Join(ErrCookieStoreNotCreated, storeErr)
	}

	if !store.IsEnabled() {
		return nil, ErrStoreDisabled
	}

	if store == nil {
		return nil, ErrStoreNotFound
	}

	return store, nil
}

// getCachedSession retrieves the cached session from the request's context.
// If the session is not found, it returns an error.
func getCachedSession(r *http.Request, name string) *Session {
	if r == nil {
		return nil
	}

	registry, ok := r.Context().Value(sessionRegistryKey).(map[string]*Session)
	if !ok {
		return nil
	}

	session, exists := registry[name]
	if !exists {
		return nil
	}

	return session
}

// setCachedSession stores a session in the request's session registry.
// It returns a new request instance with the updated context.
func setCachedSession(r *http.Request, session *Session) error {
	if r == nil {
		return errors.New("a request must be specified to cache the specified session")
	}

	registry, ok := r.Context().Value(sessionRegistryKey).(map[string]*Session)
	if !ok {
		// If the registry does not exist, create a new one.
		registry = make(map[string]*Session)
	}

	// Store the session in the registry using session.name as the key.
	registry[session.name] = session

	*r = *r.WithContext(context.WithValue(r.Context(), sessionRegistryKey, registry))

	return nil
}
