// session_test.go tests code from the session.go file
package session

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

// MockSessionStore implements the SessionStore interface for testing purposes.
type mockSessionStore struct{}

func (m *mockSessionStore) Get(r *http.Request, name string) (*Session, error) {
	return nil, nil
}

func (m *mockSessionStore) New(r *http.Request, name string) (*Session, error) {
	return nil, nil
}

func (m *mockSessionStore) Save(w http.ResponseWriter, r *http.Request, session *Session) error {
	return nil
}

func (m *mockSessionStore) End(w http.ResponseWriter, r *http.Request, session *Session) error {
	return nil
}

func (m *mockSessionStore) IsEnabled() bool {
	return true
}

func TestNewSession(t *testing.T) {
	mockStore := &mockSessionStore{} // Create a mock session store
	sessionName := "testSession"

	session := newSession(mockStore, sessionName)

	// Test: ID should not be empty
	if session.ID == "" {
		t.Errorf("Expected non-empty session ID, got empty string")
	}

	// Test: Name should match input
	if session.Name != sessionName {
		t.Errorf("Expected session name %q, got %q", sessionName, session.Name)
	}

	// Test: Store should be assigned correctly
	if session.Store != mockStore {
		t.Errorf("Expected session store to be assigned")
	}

	// Test: IsNew should be true
	if !session.IsNew {
		t.Errorf("Expected IsNew to be true, got false")
	}

	// Test: Data map should be initialized and empty
	if session.Data == nil {
		t.Errorf("Expected Data map to be initialized, got nil")
	}

	if len(session.Data) != 0 {
		t.Errorf("Expected Data map to be empty, got length %d", len(session.Data))
	}
}

// Test for getCachedSession

func TestGetCachedSession_NilRequest(t *testing.T) {
	session := getCachedSession(nil, "test")
	if session != nil {
		t.Errorf("expected nil session when request is nil, got %v", session)
	}
}

func TestGetCachedSession_NoRegistry(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	session := getCachedSession(req, "test")
	if session != nil {
		t.Errorf("expected nil session when no registry exists, got %v", session)
	}
}

func TestGetCachedSession_NotFound(t *testing.T) {
	// Create a registry with a session for a different key.
	registry := map[string]*Session{
		"other": {Name: "other"},
	}
	ctx := context.WithValue(context.Background(), sessionRegistryKey, registry)
	req, _ := http.NewRequest("GET", "/", nil)
	req = req.WithContext(ctx)

	session := getCachedSession(req, "test")
	if session != nil {
		t.Errorf("expected nil session when key is not found, got %v", session)
	}
}

func TestGetCachedSession_Found(t *testing.T) {
	expected := &Session{Name: "test"}
	registry := map[string]*Session{
		"test": expected,
	}
	ctx := context.WithValue(context.Background(), sessionRegistryKey, registry)
	req, _ := http.NewRequest("GET", "/", nil)
	req = req.WithContext(ctx)

	session := getCachedSession(req, "test")
	if session != expected {
		t.Errorf("expected session %v, got %v", expected, session)
	}
}

// Tests for setCachedSession

func TestSetCachedSession_NilRequest(t *testing.T) {
	sess := &Session{Name: "test"}
	err := setCachedSession(nil, sess)
	if err == nil {
		t.Fatalf("expected error when request is nil")
	}

	var reqErr *ErrRequestNotSpecified
	if !errors.As(err, &reqErr) {
		t.Errorf("expected error of type ErrRequestNotSpecified, got %T", err)
	}
}

func TestSetCachedSession_NewRegistry(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	sess := &Session{Name: "test"}

	err := setCachedSession(req, sess)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// After setting the cached session, we should be able to retrieve it.
	cached := getCachedSession(req, "test")
	if cached != sess {
		t.Errorf("expected cached session to equal set session, got %v", cached)
	}
}

func TestSetCachedSession_ExistingRegistry(t *testing.T) {
	// Create an initial registry with one session.
	existingSess := &Session{Name: "existing"}
	registry := map[string]*Session{
		"existing": existingSess,
	}
	ctx := context.WithValue(context.Background(), sessionRegistryKey, registry)
	req, _ := http.NewRequest("GET", "/", nil)
	req = req.WithContext(ctx)

	// Add a new session to the existing registry.
	newSess := &Session{Name: "new"}
	err := setCachedSession(req, newSess)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Verify that the new session is now cached.
	if got := getCachedSession(req, "new"); got != newSess {
		t.Errorf("expected new session to be cached, got %v", got)
	}

	// Verify that the existing session remains unchanged.
	if got := getCachedSession(req, "existing"); got != existingSess {
		t.Errorf("expected existing session to still be cached, got %v", got)
	}
}
