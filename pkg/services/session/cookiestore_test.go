// cookiestore_test.go provides tests for the CookieStore implementation of the
// SessionStore interface
package session

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/trentnix/hyperserver/config"
)

// setupCookieStore creates and returns a SQLiteStore instance for testing.
func setupCookieStore(t *testing.T) *CookieStore {
	// Create a sample config that matches your config struct.
	c := &config.Config{
		HTTP: config.HTTPConfig{
			ListenHost:   "localhost",
			Port:         8081,
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 10 * time.Second,
			IdleTimeout:  120 * time.Second,
			TLS: struct {
				Enabled     bool
				Certificate string
				Key         string
			}{
				Enabled:     false,
				Certificate: "",
				Key:         "",
			},
			Session: struct {
				JwtKey    string
				TokenAge  time.Duration
				CookieAge time.Duration
				Stores    map[string]map[string]string `mapstructure:"stores"`
				Types     map[string]string            `mapstructure:"types"`
			}{
				JwtKey:    "secretKey",
				TokenAge:  1 * time.Hour,
				CookieAge: 1 * time.Hour,
				Stores: map[string]map[string]string{
					"CookieStore": {
						"enabled": "true",
					},
				},
				Types: map[string]string{},
			},
		},
	}

	store, err := NewCookieStore(c)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}

	return store
}

// Test when no cookie is present; expect a new session.
func TestCookieStore_Get_NoCookie(t *testing.T) {
	store := setupCookieStore(t)

	req := httptest.NewRequest("GET", "/", nil)
	// No cookie is set on the request.

	session, err := store.Get(req, "cookiestore_test")
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if session == nil {
		t.Fatal("expected a session object, got nil")
	}
	if !session.IsNew {
		t.Errorf("expected IsNew to be true for a brand new session")
	}
}

// Test when an invalid JWT cookie is present; expect a new session and an error.
func TestCookieStore_Get_InvalidJWT(t *testing.T) {
	store := setupCookieStore(t)

	req := httptest.NewRequest("GET", "/", nil)
	// Set a cookie with an invalid JWT value.
	req.AddCookie(&http.Cookie{
		Name:  "cookiestore_test",
		Value: "invalid.jwt.token",
	})

	session, err := store.Get(req, "cookiestore_test")
	if err == nil {
		t.Error("expected an error for invalid JWT, got nil")
	}
	if session == nil {
		t.Fatal("expected a session object even if JWT is invalid, got nil")
	}
	if !session.IsNew {
		t.Errorf("expected IsNew to be true for an invalid JWT cookie")
	}
}

// Test the valid cookie round-trip: create a session, save it, and retrieve it.
func TestCookieStore_Get_ValidCookie(t *testing.T) {
	store := setupCookieStore(t)

	// Create a new session.
	newSession, err := store.New(nil, "cookiestore_test")
	if err != nil {
		t.Fatalf("failed to create new session: %v", err)
	}
	// Add some session data.
	newSession.Data["username"] = "testUser"

	// Create a dummy request to pass into Save.
	reqForSave := httptest.NewRequest("GET", "/", nil)

	// Save the session which writes the cookie.
	w := httptest.NewRecorder()
	if err := store.Save(w, reqForSave, newSession); err != nil {
		t.Fatalf("failed to save session: %v", err)
	}

	// Extract the cookie from the response.
	resp := w.Result()
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected at least one cookie set, got none")
	}
	cookie := cookies[0]

	// Create a new request with the cookie.
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(cookie)

	// Retrieve the session.
	retrievedSession, err := store.Get(req, "cookiestore_test")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if retrievedSession == nil {
		t.Fatal("expected to retrieve the existing session, got nil")
	}
	if retrievedSession.IsNew {
		t.Error("expected IsNew to be false for a valid existing session")
	}
	if retrievedSession.Data["username"] != "testUser" {
		t.Errorf("expected session.Data[\"username\"] to be 'testUser', got %q", retrievedSession.Data["username"])
	}
}

// TestCookieStore_End_NonTLS verifies that End writes a cookie that deletes the session
// when the request is not over TLS.
func TestCookieStore_End_NonTLS(t *testing.T) {
	store := setupCookieStore(t)

	// Create a dummy session. Since tests are in the same package,
	// we can set unexported fields directly.
	session := &Session{
		Name: "cookiestore_test",
	}

	// Create a non-TLS request.
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	w := httptest.NewRecorder()

	// Call End
	if err := store.End(w, req, session); err != nil {
		t.Fatalf("End returned an unexpected error: %v", err)
	}

	// Check the resulting cookie.
	resp := w.Result()
	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie to be set, got %d", len(cookies))
	}
	cookie := cookies[0]

	if cookie.Name != session.Name {
		t.Errorf("expected cookie name %q, got %q", session.Name, cookie.Name)
	}
	if cookie.Value != "" {
		t.Errorf("expected cookie value to be empty, got %q", cookie.Value)
	}
	if cookie.MaxAge != -1 {
		t.Errorf("expected cookie MaxAge to be -1, got %d", cookie.MaxAge)
	}
	if cookie.Secure {
		t.Errorf("expected Secure to be false for non-TLS, got true")
	}
}

// TestCookieStore_End_TLS verifies that End writes a cookie with Secure set to true
// when the request is over TLS.
func TestCookieStore_End_TLS(t *testing.T) {
	store := setupCookieStore(t)

	session := &Session{
		Name: "cookiestore_test",
	}

	// Create a TLS-enabled request. httptest.NewRequest doesn't set r.TLS,
	// so we assign a dummy ConnectionState.
	req := httptest.NewRequest("GET", "https://example.com/", nil)
	req.TLS = &tls.ConnectionState{}
	w := httptest.NewRecorder()

	if err := store.End(w, req, session); err != nil {
		t.Fatalf("End returned an unexpected error: %v", err)
	}

	resp := w.Result()
	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie to be set, got %d", len(cookies))
	}
	cookie := cookies[0]

	if !cookie.Secure {
		t.Errorf("expected Secure to be true for TLS request, got false")
	}
}

// TestCookieStore_New_Valid verifies that New returns a new session when the store is configured.
func TestCookieStore_New_Valid(t *testing.T) {
	store := setupCookieStore(t)
	// Create a dummy request.
	req := httptest.NewRequest("GET", "http://example.com/", nil)

	session, err := store.New(req, "cookiestore_test")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if session == nil {
		t.Fatal("expected a session object, got nil")
	}
	// Assuming newSession sets session.name and marks new sessions as IsNew.
	if session.Name != "cookiestore_test" {
		t.Errorf("expected session name to be 'cookiestore_test', got %q", session.Name)
	}
	if !session.IsNew {
		t.Errorf("expected session.IsNew to be true for a new session")
	}
}

// TestCookieStore_Save_NewSession verifies that Save properly saves a new session,
// writes a valid JWT cookie, and sets the session expiration.
func TestCookieStore_Save_NewSession(t *testing.T) {
	store := setupCookieStore(t)
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	w := httptest.NewRecorder()

	// Create a new session.
	session, err := store.New(req, "cookiestore_test")
	if err != nil {
		t.Fatalf("failed to create new session: %v", err)
	}

	// Set some dummy session data.
	session.Data["username"] = "testUser"

	// Call Save.
	err = store.Save(w, req, session)
	if err != nil {
		t.Fatalf("Save returned an error: %v", err)
	}

	// Verify that a cookie was written.
	resp := w.Result()
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected a cookie to be set, got none")
	}
	cookie := cookies[0]

	// Check cookie properties.
	if cookie.Name != session.Name {
		t.Errorf("expected cookie name %q, got %q", session.Name, cookie.Name)
	}
	if cookie.Value == "" {
		t.Error("expected cookie value to be non-empty")
	}
	expectedMaxAge := int(store.CookieLifetime / time.Second)
	if cookie.MaxAge != expectedMaxAge {
		t.Errorf("expected cookie MaxAge to be %d, got %d", expectedMaxAge, cookie.MaxAge)
	}

	// Verify that the JWT in the cookie is valid and contains the expected claims.
	token, err := jwt.ParseWithClaims(cookie.Value, &SessionClaims{}, func(token *jwt.Token) (interface{}, error) {
		return store.JwtKey, nil
	})
	if err != nil {
		t.Errorf("failed to parse JWT from cookie: %v", err)
	}
	if !token.Valid {
		t.Error("JWT from cookie is not valid")
	}

	// Ensure that session.ExpiresAt has been set.
	if session.ExpiresAt.IsZero() {
		t.Error("expected session.ExpiresAt to be set, got zero value")
	}
}

// TestCookieStore_Save_ExistingSession verifies that saving an existing session
// (IsNew == false) updates the session without error.
func TestCookieStore_Save_ExistingSession(t *testing.T) {
	store := setupCookieStore(t)
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	w := httptest.NewRecorder()

	// Create and save a new session first.
	session, err := store.New(req, "cookiestore_test")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	session.Data["foo"] = "bar"
	if err := store.Save(w, req, session); err != nil {
		t.Fatalf("initial Save returned error: %v", err)
	}

	// Simulate an update by marking the session as existing.
	session.IsNew = false
	w = httptest.NewRecorder() // Reset the ResponseRecorder.

	// Save the updated session.
	if err := store.Save(w, req, session); err != nil {
		t.Fatalf("Save for existing session returned error: %v", err)
	}

	// Verify that a cookie was written.
	resp := w.Result()
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected a cookie to be set for updated session, got none")
	}
}
