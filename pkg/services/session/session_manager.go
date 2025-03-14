// session.go wraps session management so that whether a JWT or server-managed session is used is
// abstracted from the caller. Currently, the implementation uses a JWT but this could be abstracted
// so that the SessionManager uses a single interface irrespective of how a session is managed.
package session

import (
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/logger"
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

	defaultStore = "default"
)

var (
	// singleton instance of a SessionManager service
	sessionManager *SessionManager
	// used to manage the singleton
	once sync.Once
)

// InitializeSessionManager creates a new instance of the global SessionManager and
func InitializeSessionManager(c *config.Config) *SessionManager {
	if sessionManager != nil {
		return sessionManager
	}

	once.Do(func() {
		// set the application's SessionManager instance
		sessionManager = &SessionManager{
			config: c,
			Stores: c.HTTP.Session.Stores,
			Types:  c.HTTP.Session.Types,
		}
	})

	return sessionManager
}

// GetContentManager returns the global SessionManager service if it exists
func GetSessionManager() *SessionManager {
	return sessionManager
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
		return nil, NewErrSessionStoreNotFound(err)
	}

	// retrieve the session from the request
	session, _ = store.Get(r, name)
	if session == nil {
		// if the request doesn't have a session, return a new session
		session, err = m.New(r, name)
		if err != nil || session == nil {
			return nil, NewErrSessionCouldNotBeCreated(err)
		}
	}

	// cache the session so any subsequent retrieval is efficient
	cacheSessionErr := setCachedSession(r, session)
	if cacheSessionErr != nil {
		logger.LogRequestError(r, fmt.Errorf("there was an error caching the retrieved session: %w", cacheSessionErr))
	}

	return session, nil
}

// New returns a new Session instance irrespective of whether one already exists with the
// specified name. If there is an existing session with the same name, it is ignored.
func (m *SessionManager) New(r *http.Request, name string) (*Session, error) {
	store, err := m.getStore(name)
	if err != nil {
		return nil, err
	}

	session := newSession(store, name)

	// cache the session so any subsequent retrieval is efficient
	cacheSessionErr := setCachedSession(r, session)
	if cacheSessionErr != nil {
		logger.LogRequestError(r, fmt.Errorf("there was an error caching the specified session: %w", cacheSessionErr))
	}

	return session, nil
}

// getStore retrieves the session store that has been configured for the specified session name.
func (m *SessionManager) getStore(name string) (SessionStore, error) {
	storeType, ok := m.Types[name]
	if !ok {
		// the session type isn't explicitly defined so check for a default configuration
		if storeType, ok = m.Types[defaultStore]; !ok {
			return nil, NewErrSessionStoreNotFound(nil)
		}
	}

	var store SessionStore
	var storeErr error

	switch {
	case strings.EqualFold(storeType, cookieStoreName):
		store, storeErr = NewCookieStore(m.config)
		if storeErr != nil {
			return nil, NewErrCookieStoreNotCreated(storeErr)
		}
	case strings.EqualFold(storeType, sqliteStoreName):
		store, storeErr = NewSQLiteStore(m.config)
		if storeErr != nil {
			return nil, NewErrSQLiteStoreNotCreated(storeErr)
		}
	}

	if !store.IsEnabled() {
		return nil, NewErrStoreDisabled(fmt.Errorf("store type: %s", storeType))
	}

	if store == nil {
		return nil, NewErrSessionStoreNotFound(nil)
	}

	return store, nil
}
