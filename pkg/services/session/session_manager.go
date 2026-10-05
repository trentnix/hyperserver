// Package session manages named sessions through cookie and SQLite stores.
// Attach a SessionManager to each request before calling Get or New. Session data
// is size-limited JSON, and cookie payloads are signed, not encrypted. Each manager
// owns its initialized providers. SQLite End revokes the stored session.
package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/logger"
)

type (
	// SessionManager is used to manage sessions
	SessionManager struct {
		// Types maps session names to provider names, with "default" as the fallback.
		// Configure mappings before serving requests. Providers are fixed at construction.
		Types   map[string]string
		stores  map[string]SessionStore
		closers []io.Closer
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

// NewSessionManager validates configuration and initializes each selected provider
// once. It copies session mappings and releases acquired resources on failure.
// Call Close after all consumers stop. Requests never initialize providers.
func NewSessionManager(ctx context.Context, c *config.Config) (*SessionManager, error) {
	if c == nil {
		return nil, fmt.Errorf("session configuration is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := ValidateConfig(c); err != nil {
		return nil, err
	}
	m := &SessionManager{Types: maps.Clone(c.HTTP.Session.Types), stores: make(map[string]SessionStore)}
	selected := make(map[string]bool)
	for _, name := range m.Types {
		selected[strings.ToLower(name)] = true
	}
	for _, name := range slices.Sorted(maps.Keys(selected)) {
		var store SessionStore
		var err error
		switch name {
		case strings.ToLower(cookieStoreName):
			store, err = NewCookieStore(c)
		case strings.ToLower(sqliteStoreName):
			store, err = NewSQLiteStore(ctx, c)
		}
		if err != nil {
			return nil, errors.Join(fmt.Errorf("initialize session store %s: %w", name, err), m.Close())
		}
		m.stores[name] = store
		if closer, ok := store.(io.Closer); ok {
			m.closers = append(m.closers, closer)
		}
	}
	return m, nil
}

// Close releases owned provider resources in reverse initialization order.
// Requests and background consumers must have stopped before Close is called.
func (m *SessionManager) Close() error {
	var err error
	for i := len(m.closers) - 1; i >= 0; i-- {
		err = errors.Join(err, m.closers[i].Close())
	}
	return err
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

	store := m.stores[strings.ToLower(storeType)]
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
