package session

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	hs_errors "github.com/trentnix/hyperserver/pkg/errors"
)

type (
	// Session is used to manage a user or usage session
	Session struct {
		ID string
		// Data contains JSON-compatible values. The JSON decoder uses json.Number.
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

// NewSession creates an empty, unsaved session for a store implementation.
// Request handlers should use New to also cache the session in the request.
func NewSession(s SessionStore, name string) *Session {
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

// LoadSession restores a store's base64-encoded data using the selected serializer.
// A nil serializer selects JSON. Size limits apply before calling the serializer.
// Providers must validate identity and expiration before calling it.
func LoadSession(id string, name string, expiration time.Time, s SessionStore, value string, serializer Serializer) (*Session, error) {
	session := Session{
		ID:        id,
		Name:      name,
		ExpiresAt: expiration,
		Store:     s,
		IsNew:     false,
	}

	if len(value) > MaxEncodedSessionSize {
		return nil, ErrSessionTooLarge
	}
	byteValue, base64DecodeErr := base64.URLEncoding.Strict().DecodeString(value)
	if base64DecodeErr != nil {
		return nil, base64DecodeErr
	}

	if len(byteValue) > MaxSessionDataSize {
		return nil, ErrSessionTooLarge
	}

	if serializer == nil {
		serializer = JSONSerializer{}
	}
	sessionData, err := serializer.Decode(byteValue)
	if err != nil {
		return nil, err
	}
	if sessionData == nil {
		return nil, hs_errors.WithCause(ErrSessionInvalid, errors.New("session serializer returned nil data"))
	}

	session.Data = sessionData
	return &session, nil
}

// Save saves the specified session to its corresponding store and writes the
// session to a http cookie.
func (s *Session) Save(w http.ResponseWriter, r *http.Request) error {
	if s.Store == nil {
		return ErrSessionStoreNotFound
	}

	// save the session to the store
	return s.Store.Save(w, r, s)
}

// RotatingStore atomically replaces a stored session with a new ID and data.
// A failure must leave storage unchanged and must not issue a cookie.
// An expired or revoked stored session must not be rotated. Concurrent rotation
// of the same stored session must succeed at most once. End must revoke it, and
// Save must not recreate it after revocation. Implement this capability only when
// the provider can enforce all these guarantees.
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
		return ErrRequestNotSpecified
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
// value with a new session from the provider. Replacement creation happens before
// revocation, so either failure leaves local state unchanged.
func (s *Session) End(w http.ResponseWriter, r *http.Request) error {
	if s.Store == nil {
		return ErrSessionStoreNotFound
	}

	if r == nil {
		return ErrRequestNotSpecified
	}
	replacement, err := createSession(r, s.Store, s.Name)
	if err != nil {
		return err
	}
	if err := s.Store.End(w, r, s); err != nil {
		return err
	}
	clear(s.Data)
	*s = *replacement
	return setCachedSession(r, s)
}

// DecodeValue decodes a JSON-compatible Data value into target, which must be a
// pointer. A missing key leaves target unchanged. Use it for numbers and structured
// values. This JSON conversion is independent of the storage serializer.
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

// EncodedData serializes values into URL-safe base64, limited to MaxSessionDataSize
// bytes before base64 encoding. A nil serializer selects JSON. Encoding does not encrypt data.
func (s *Session) EncodedData(serializer Serializer) (string, error) {
	data := s.Data
	if data == nil {
		data = map[string]any{}
	}
	if serializer == nil {
		serializer = JSONSerializer{}
	}
	encoded, err := serializer.Encode(data)
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
		return hs_errors.WithCause(ErrRequestNotSpecified, errors.New("unable to set the session to the session cache"))
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
