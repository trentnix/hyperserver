package main

import (
	"errors"
	"net/http"
	"sync"
	"time"

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
	if s.closes != 0 {
		return nil, errors.New("test session store closed")
	}
	if s.fail != nil {
		return nil, s.fail
	}
	cookie, err := r.Cookie(name)
	if err == nil {
		record, ok := s.records[cookie.Value]
		if ok && record.name == name && time.Now().Before(record.expires) {
			return session.LoadSession(cookie.Value, name, record.expires, s, record.data)
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
	encoded, err := value.EncodedData()
	if err != nil {
		return err
	}
	value.ExpiresAt = time.Now().Add(time.Hour)
	s.records[value.ID] = replacementSessionRecord{name: value.Name, data: encoded, expires: value.ExpiresAt}
	value.IsNew = false
	http.SetCookie(w, &http.Cookie{Name: value.Name, Value: value.ID, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
	return nil
}

func (s *replacementSessionStore) Rotate(w http.ResponseWriter, r *http.Request, old *session.Session, data map[string]any) (*session.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
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
