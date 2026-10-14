package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
	"github.com/trentnix/hyperserver/internal/test/sessiontest"
	"github.com/trentnix/hyperserver/pkg/services/session"
)

// This small in-memory test store exercises session replacement through real HTTP
// flows. It is not a supported provider or a production implementation.
type replacementSessionStore struct {
	mu        sync.Mutex
	records   map[string]replacementSessionRecord
	rotations int
	closes    int
	fail      error
}

type replacementSessionRecord struct {
	name, data string
	expires    time.Time
}

func (*replacementSessionStore) IsEnabled() bool { return true }

func (s *replacementSessionStore) New(r *http.Request, name string) (*session.Session, error) {
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	return session.NewSession(s, name), nil
}

func (s *replacementSessionStore) Get(r *http.Request, name string) (*session.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	if s.closes != 0 {
		return nil, errors.New("test session store closed")
	}
	if s.fail != nil {
		return nil, s.fail
	}
	cookie, err := r.Cookie(name)
	if err == nil {
		if _, err := uuid.Parse(cookie.Value); err != nil {
			return nil, session.NewErrInvalidToken(err)
		}
		record, ok := s.records[cookie.Value]
		if ok && record.name == name && time.Now().Before(record.expires) {
			return session.LoadSession(cookie.Value, name, record.expires, s, record.data, nil)
		}
	}
	return s.New(r, name)
}

func (s *replacementSessionStore) Save(w http.ResponseWriter, r *http.Request, value *session.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.save(w, r, value)
}

func (s *replacementSessionStore) save(w http.ResponseWriter, r *http.Request, value *session.Session) error {
	if err := r.Context().Err(); err != nil {
		return err
	}
	if s.closes != 0 {
		return errors.New("test session store closed")
	}
	encoded, err := value.EncodedData(nil)
	if err != nil {
		return err
	}
	stored, exists := s.records[value.ID]
	expires := stored.expires
	if value.IsNew {
		if exists {
			return session.NewErrSessionInvalid(errors.New("session ID already exists"))
		}
		expires = time.Now().Add(time.Hour)
	} else if !exists {
		return session.NewErrSessionInvalid(errors.New("session was revoked"))
	}

	s.records[value.ID] = replacementSessionRecord{name: value.Name, data: encoded, expires: expires}
	value.ExpiresAt = expires
	value.IsNew = false
	http.SetCookie(w, &http.Cookie{Name: value.Name, Value: value.ID, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
	return nil
}

func (s *replacementSessionStore) Rotate(w http.ResponseWriter, r *http.Request, old *session.Session, data map[string]any) (*session.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	if !old.IsNew {
		stored, exists := s.records[old.ID]
		if !exists || !time.Now().Before(stored.expires) {
			return nil, session.NewErrSessionInvalid(nil)
		}
	}

	replacement := session.NewSession(s, old.Name)
	replacement.Data = data
	if err := s.save(w, r, replacement); err != nil {
		return nil, err
	}
	delete(s.records, old.ID)
	s.rotations++
	return replacement, nil
}

func (s *replacementSessionStore) End(w http.ResponseWriter, r *http.Request, value *session.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := r.Context().Err(); err != nil {
		return err
	}
	delete(s.records, value.ID)
	session.ExpireCookie(w, r, value.Name)
	return nil
}

func (s *replacementSessionStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closes++
	return nil
}

func TestMemorySessionContract(t *testing.T) {
	sessiontest.Run(t, func(t *testing.T) session.SessionStore {
		store := &replacementSessionStore{records: make(map[string]replacementSessionRecord)}
		t.Cleanup(func() { store.Close() })
		return store
	})
}

func TestMemorySessionExpiryBoundary(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := &replacementSessionStore{records: make(map[string]replacementSessionRecord)}
		t.Cleanup(func() { store.Close() })
		s := session.NewSession(store, "account")
		s.Data["identity"] = "account"
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if err := store.Save(httptest.NewRecorder(), r, s); err != nil {
			t.Fatal(err)
		}
		r.AddCookie(&http.Cookie{Name: s.Name, Value: s.ID})

		time.Sleep(time.Until(s.ExpiresAt))
		expired, err := store.Get(r, s.Name)
		if err != nil || expired == nil || !expired.IsNew || len(expired.Data) != 0 {
			t.Fatalf("expired session restored identity: %+v, %v", expired, err)
		}
		w := httptest.NewRecorder()
		if next, err := store.Rotate(w, r, s, s.Data); next != nil || err == nil {
			t.Fatalf("expired rotation = %+v, %v", next, err)
		}
		if len(w.Header().Values("Set-Cookie")) != 0 {
			t.Fatal("expired rotation issued a cookie")
		}
	})
}
