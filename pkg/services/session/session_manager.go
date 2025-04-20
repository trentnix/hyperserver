// session.go wraps session management so that whether a JWT or server-managed session is used is
// abstracted from the caller. Currently, the implementation uses a JWT but this could be abstracted
// so that the SessionManager uses a single interface irrespective of how a session is managed.
package session

import (
	"context"
	"fmt"
	"net/http"
	"strings"

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

// GetSessionManager returns the SessionManager saved to the request context
func GetSessionManager(r *http.Request) *SessionManager {
	return getSessionManagerFromContext(r.Context())
}

func NewSessionManager(c *config.Config) *SessionManager {
	sessionManager := &SessionManager{
		config: c,
		Stores: c.HTTP.Session.Stores,
		Types:  c.HTTP.Session.Types,
	}

	return sessionManager
}

// Get returns any current sessions specified in the request with the specified name.
// If a session is not found, a new session will be returned.
func Get(r *http.Request, name string) (*Session, error) {
	var session *Session

	session = getCachedSession(r, name)
	if session != nil {
		return session, nil
	}

	sm := GetSessionManager(r)
	if sm == nil {
		return nil, NewErrSessionManagerNotFound(nil)
	}

	store, err := sm.getStore(name)
	if err != nil {
		return nil, NewErrSessionStoreNotFound(err)
	}

	// retrieve the session from the request
	session, _ = store.Get(r, name)
	if session == nil {
		// if the request doesn't have a session, return a new session
		session, err = New(r, name)
		if err != nil || session == nil {
			return nil, NewErrSessionCouldNotBeCreated(err)
		}
	}

	// cache the session so any subsequent retrieval is efficient
	cacheSessionErr := setCachedSession(r, session)
	if cacheSessionErr != nil {
		logger.LogRequestError(r, cacheSessionErr)
	}

	return session, nil
}

// New returns a new Session instance irrespective of whether one already exists with the
// specified name. If there is an existing session with the same name, it is ignored.
func New(r *http.Request, name string) (*Session, error) {
	sm := GetSessionManager(r)
	if sm == nil {
		return nil, NewErrSessionManagerNotFound(nil)
	}

	store, err := sm.getStore(name)
	if err != nil {
		return nil, err
	}

	session := newSession(store, name)

	// cache the session so any subsequent retrieval is efficient
	cacheSessionErr := setCachedSession(r, session)
	if cacheSessionErr != nil {
		logger.LogRequestError(r, cacheSessionErr)
	}

	return session, nil
}

// getStore retrieves the session store that has been configured for the specified session name.
func (m *SessionManager) getStore(sessionName string) (SessionStore, error) {
	storeType, ok := m.Types[sessionName]
	if !ok {
		// the session type isn't explicitly defined so check for a default configuration
		if storeType, ok = m.Types[defaultStore]; !ok {
			return nil, NewErrSessionStoreNotFound(fmt.Errorf("session name: %s", sessionName))
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
		return nil, NewErrSessionStoreNotFound(fmt.Errorf("store type: %s", storeType))
	}

	return store, nil
}

// AddSessionManagerToRequestContext adds a SessionManager instance to the specified request context
func AddSessionManagerToRequestContext(r *http.Request, s *SessionManager) *http.Request {
	ctx := r.Context()
	ctx = context.WithValue(ctx, SessionContextKey, s)
	return r.WithContext(ctx)
}

// getSessionManagerFromContext retrieves a SessionManager instance from the specified context
func getSessionManagerFromContext(ctx context.Context) *SessionManager {
	if ctx == nil {
		return nil
	}

	s, ok := ctx.Value(SessionContextKey).(*SessionManager)
	if !ok || s == nil {
		return nil
	}

	return s
}
