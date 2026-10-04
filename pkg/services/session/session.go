package session

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
)

type (
	// Session is used to manage a user or usage session
	Session struct {
		ID string
		// Data contains JSON-compatible values. Loaded numbers use json.Number.
		// Cookie stores expose these values to the browser. Never store secrets there.
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

	if len(value) > maxEncodedSessionSize {
		return nil, ErrSessionTooLarge
	}
	byteValue, base64DecodeErr := base64.URLEncoding.Strict().DecodeString(value)
	if base64DecodeErr != nil {
		return nil, base64DecodeErr
	}

	if len(byteValue) > MaxSessionDataSize {
		return nil, ErrSessionTooLarge
	}

	var sessionData map[string]any
	decoder := json.NewDecoder(bytes.NewReader(byteValue))
	decoder.UseNumber()
	if err := decoder.Decode(&sessionData); err != nil {
		return nil, err
	}
	if sessionData == nil {
		return nil, NewErrSessionInvalid(errors.New("session data must be a JSON object"))
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, NewErrSessionInvalid(errors.New("unexpected data after session object"))
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

// RotatingStore atomically replaces a stored session with a new ID and data.
// A failure must leave the old session valid and must not issue a cookie.
type RotatingStore interface {
	Rotate(http.ResponseWriter, *http.Request, *Session, map[string]any) (*Session, error)
}

// Rotate replaces the session and its request-local cache only after the store
// commits the replacement. Providers without server-side revocation are rejected.
func (s *Session) Rotate(w http.ResponseWriter, r *http.Request, data map[string]any) error {
	store, ok := s.Store.(RotatingStore)
	if !ok {
		return errors.New("session store does not support revocable session rotation")
	}
	if r == nil {
		return NewErrRequestNotSpecified(nil)
	}

	replacement, err := store.Rotate(w, r, s, data)
	if err != nil {
		return err
	}

	clear(s.Data)
	*s = *replacement
	return setCachedSession(r, s)
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

// DecodeValue decodes a JSON-compatible Data value into target, which must be a
// pointer. A missing key leaves target unchanged. Use it for numbers and structured
// values, whose Go types are not preserved across JSON storage.
func (s *Session) DecodeValue(key string, target any) error {
	value, ok := s.Data[key]
	if !ok {
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(encoded) > MaxSessionDataSize {
		return ErrSessionTooLarge
	}
	return json.Unmarshal(encoded, target)
}

// EncodedData returns JSON in URL-safe base64, limited to MaxSessionDataSize
// bytes before base64 encoding. Encoding does not encrypt data. Gob is unsupported.
func (s *Session) EncodedData() (string, error) {
	data := s.Data
	if data == nil {
		data = map[string]any{}
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	if len(encoded) > MaxSessionDataSize {
		return "", ErrSessionTooLarge
	}

	return base64.URLEncoding.EncodeToString(encoded), nil
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
