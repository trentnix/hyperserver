package session

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/gob"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
)

type (
	// Session is used to manage a user or usage session
	Session struct {
		ID        string
		Data      map[string]any
		Name      string
		Store     SessionStore
		IsNew     bool
		ExpiresAt time.Time
	}

	contextKey string
)

const (
	// SessionContextKey identifies the SessionManager stored in a request context.
	SessionContextKey contextKey = "auth-user"
)

// newSession returns a new Session instance with the specified session name serialized
// to the specified session store. newSession is intended for use only within the
// session package by a session store implementation.
func newSession(s SessionStore, name string) *Session {
	sessionID := uuid.New().String()
	session := Session{
		ID:    sessionID,
		Name:  name,
		Store: s,
		IsNew: true,
	}

	session.Data = make(map[string]any)
	return &session
}

// loadSession takes existing session data and creates a Session instance with the provided
// values
func loadSession(id string, name string, expiration time.Time, s SessionStore, value string) (*Session, error) {
	session := Session{
		ID:        id,
		Name:      name,
		ExpiresAt: expiration,
		Store:     s,
		IsNew:     false,
	}

	byteValue, base64DecodeErr := base64.URLEncoding.DecodeString(value)
	if base64DecodeErr != nil {
		return nil, base64DecodeErr
	}

	var sessionData map[string]any
	if err := gob.NewDecoder(bytes.NewReader(byteValue)).Decode(&sessionData); err != nil {
		return nil, err
	}

	session.Data = sessionData
	return &session, nil
}

// Save saves the specified session to its corresponding store and writes the
// session to a http cookie.
func (s *Session) Save(w http.ResponseWriter, r *http.Request) error {
	if s.Store == nil {
		return NewErrSessionStoreNotFound(nil)
	}

	// save the session to the store
	return s.Store.Save(w, r, s)
}

// End delegates revocation to the store, then replaces this session and its cached
// value with a new empty session. A failed revocation leaves local state unchanged.
func (s *Session) End(w http.ResponseWriter, r *http.Request) error {
	if s.Store == nil {
		return NewErrSessionStoreNotFound(nil)
	}

	if r == nil {
		return NewErrRequestNotSpecified(nil)
	}
	if err := s.Store.End(w, r, s); err != nil {
		return err
	}
	clear(s.Data)
	*s = *newSession(s.Store, s.Name)
	return setCachedSession(r, s)
}

// EncodedData returns Data encoded as gob in URL-safe base64.
// Encoding does not encrypt the data, and custom concrete values may require gob registration.
func (s *Session) EncodedData() (string, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(s.Data); err != nil {
		return "", err
	}

	return base64.URLEncoding.EncodeToString(buf.Bytes()), nil
}

// getCachedSession returns the request-cached session, or nil if it is absent.
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

// setCachedSession caches the session and replaces r's context in place.
func setCachedSession(r *http.Request, session *Session) error {
	if r == nil {
		return NewErrRequestNotSpecified(errors.New("unable to set the session to the session cache"))
	}

	registry, ok := r.Context().Value(sessionRegistryKey).(map[string]*Session)
	if !ok {
		// If the registry does not exist, create a new one.
		registry = make(map[string]*Session)
	}

	// Store the session in the registry using session.name as the key.
	registry[session.Name] = session

	*r = *r.WithContext(context.WithValue(r.Context(), sessionRegistryKey, registry))

	return nil
}
