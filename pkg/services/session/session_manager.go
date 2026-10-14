// Package session manages named sessions through application-supplied stores.
// Attach a SessionManager to each request before calling Get or New. Session data
// uses bounded serialization, defaulting to JSON. Each manager owns its initialized providers.
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

// StoreFactory initializes a session store. On failure it must release any
// partially acquired resources. On success the manager owns the returned store.
type StoreFactory func(context.Context) (SessionStore, error)

// NewSessionManager validates selections and initializes each selected provider
// once. It copies session mappings and releases acquired resources on failure.
// Call Close after all consumers stop. Requests never initialize providers.
// Provider names are case-insensitive. Unselected factories are not called.
func NewSessionManager(ctx context.Context, types map[string]string, factories map[string]StoreFactory) (*SessionManager, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	providers := make(map[string]StoreFactory, len(factories))
	for _, name := range slices.Sorted(maps.Keys(factories)) {
		key := strings.ToLower(name)
		if key == "" || strings.TrimSpace(key) != key || factories[name] == nil {
			return nil, fmt.Errorf("invalid session store factory %q", name)
		}
		if _, exists := providers[key]; exists {
			return nil, fmt.Errorf("duplicate session store factory %q", name)
		}
		providers[key] = factories[name]
	}

	m := &SessionManager{Types: maps.Clone(types), stores: make(map[string]SessionStore)}
	selected := make(map[string]bool)
	for _, sessionName := range slices.Sorted(maps.Keys(m.Types)) {
		name := m.Types[sessionName]
		if providers[strings.ToLower(name)] == nil {
			return nil, fmt.Errorf("session %q selects unknown store %q", sessionName, name)
		}
		selected[strings.ToLower(name)] = true
	}

	for _, name := range slices.Sorted(maps.Keys(selected)) {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(err, m.Close())
		}
		store, err := providers[name](ctx)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("initialize session store %s: %w", name, err), m.Close())
		}
		if closer, ok := store.(io.Closer); ok {
			m.closers = append(m.closers, closer)
		}
		if store == nil || !store.IsEnabled() {
			return nil, errors.Join(fmt.Errorf("session store %q is missing or disabled", name), m.Close())
		}
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(err, m.Close())
		}
		m.stores[name] = store
	}
	return m, nil
}

// Close releases owned provider resources in reverse initialization order.
// Requests and background consumers must have stopped before Close is called.
func (m *SessionManager) Close() error {
	var err error
	closers := m.closers
	m.closers = nil
	for i := len(closers) - 1; i >= 0; i-- {
		err = errors.Join(err, closers[i].Close())
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

	store, err := sm.Store(name)
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

	store, err := sm.Store(name)
	if err != nil {
		return nil, err
	}

	session := NewSession(store, name)

	// cache the session so any subsequent retrieval is efficient
	cacheSessionErr := setCachedSession(r, session)
	if cacheSessionErr != nil {
		logger.LogRequestError(r, cacheSessionErr)
	}

	return session, nil
}

// Store returns the provider selected for a session name, falling back to "default".
// Callers borrow the store and must not close it.
func (m *SessionManager) Store(sessionName string) (SessionStore, error) {
	if m == nil {
		return nil, NewErrSessionManagerNotFound(nil)
	}
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
