// Package session manages named sessions through cookie and SQLite stores.
// Attach a SessionManager to each request before calling Get or New. Session data
// is size-limited JSON, and cookie payloads are signed, not encrypted. SQLite store setup
// currently shares process-wide state. SQLite End revokes the stored session.
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

		// Stores maps provider names to their configuration options.
		Stores map[string]map[string]string
		// Types maps session names to provider names, with "default" as the fallback.
		Types map[string]string
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

// NewSessionManager retains the session configuration and store mappings from c.
// It does not validate configuration or open a store.
func NewSessionManager(c *config.Config) *SessionManager {
	sessionManager := &SessionManager{
		config: c,
		Stores: c.HTTP.Session.Stores,
		Types:  c.HTTP.Session.Types,
	}

	return sessionManager
}

// Get returns a request-cached or stored session, or creates a new one when absent
// or expired. A SessionManager must be attached to r. Load failures return an error
// without caching or replacing the failed session.
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
	session, err = store.Get(r, name)
	if err != nil {
		return nil, err
	}
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

// New creates and caches an unsaved session with a new ID.
// It replaces the request-local session of the same name but does not revoke the old one.
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
	default:
		return nil, NewErrSessionStoreNotFound(fmt.Errorf("store type: %s", storeType))
	}

	if !store.IsEnabled() {
		return nil, NewErrStoreDisabled(fmt.Errorf("store type: %s", storeType))
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
