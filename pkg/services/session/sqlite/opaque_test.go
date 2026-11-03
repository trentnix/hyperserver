package sqlite

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/trentnix/hyperserver/internal/opaquetoken"
	sessions "github.com/trentnix/hyperserver/pkg/services/session"
)

func TestOpaqueSessionStorageAndNameBinding(t *testing.T) {
	store := revocationStore(t)
	s, r := savedSession(t, store)
	var id string
	if err := store.db.QueryRow(`SELECT id FROM sessions`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if id == s.ID || id != sessionKey(s.ID, s.Name) || !opaquetoken.Valid(s.ID) {
		t.Fatal("raw token stored or invalid identifier")
	}
	for _, name := range []string{s.Name, "different-name"} {
		request := httptest.NewRequest("GET", "/", nil)
		request.AddCookie(&http.Cookie{Name: name, Value: s.ID})
		got, err := store.Get(request, name)
		if err != nil {
			t.Fatal(err)
		}
		if (name == s.Name) == got.IsNew {
			t.Fatal("session name binding failed")
		}
	}
	request := httptest.NewRequest("GET", "/", nil)
	request.AddCookie(&http.Cookie{Name: s.Name, Value: id})
	if got, err := store.Get(request, s.Name); got != nil || !errors.Is(err, sessions.ErrInvalidToken) {
		t.Fatal("stored hash usable as a cookie")
	}
	if err := store.End(httptest.NewRecorder(), r, s); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Get(r, s.Name); err != nil || !got.IsNew {
		t.Fatal("revoked cookie restored a session", err)
	}
}

func TestLegacySessionCookieCannotRestorePlaintextRow(t *testing.T) {
	store := revocationStore(t)
	expires := time.Now().Add(time.Hour)
	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"id": "old-id", "name": "test", "purpose": "session", "exp": expires.Unix()}).SignedString([]byte("old-key"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`INSERT INTO sessions (id, session, expires_at) VALUES ('old-id', 'e30=', ?)`, expires); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: "test", Value: raw})
	if got, err := store.Get(r, "test"); got != nil || !errors.Is(err, sessions.ErrInvalidToken) {
		t.Fatal("legacy token restored a session")
	}
}
