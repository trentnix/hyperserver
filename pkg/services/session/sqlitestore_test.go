package session

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
)

// setupSQLiteStore creates and returns a SQLiteStore instance for testing.
func setupSQLiteStore(t *testing.T) *SQLiteStore {
	// Create a sample config that matches your config struct.
	c := &config.Config{
		HTTP: config.HTTPConfig{
			Hostname:     "localhost",
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
					"SQLiteStore": {
						"enabled":      "true",
						"connection":   "file::memory:?cache=shared",
						"sessiontable": "sessions_test",
					},
				},
				Types: map[string]string{},
			},
		},
	}

	store, err := NewSQLiteStore(c)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}

	return store
}

// Test when no cookie is present; expect a new session.
func TestSQLiteStore_Get_NoCookie(t *testing.T) {
	store := setupSQLiteStore(t)

	req := httptest.NewRequest("GET", "/", nil)
	// No cookie is set on the request.

	session, err := store.Get(req, "user")
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
func TestSQLiteStore_Get_InvalidJWT(t *testing.T) {
	store := setupSQLiteStore(t)

	req := httptest.NewRequest("GET", "/", nil)
	// Set a cookie with an invalid JWT value.
	req.AddCookie(&http.Cookie{
		Name:  "user",
		Value: "invalid.jwt.token",
	})

	session, err := store.Get(req, "user")
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
func TestSQLiteStore_Get_ValidCookie(t *testing.T) {
	store := setupSQLiteStore(t)

	// Create a new session.
	newSession, err := store.New(nil, "user")
	if err != nil {
		t.Fatalf("failed to create new session: %v", err)
	}
	// Add some session data.
	newSession.Data["username"] = "testUser"

	// Create a dummy request to pass into Save.
	reqForSave := httptest.NewRequest("GET", "/", nil)

	// Save the session which writes the cookie.
	w := httptest.NewRecorder()
	if err := store.Save(reqForSave, w, newSession); err != nil {
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
	retrievedSession, err := store.Get(req, "user")
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
