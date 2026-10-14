package sqlite

import (
	"context"
	"errors"
	"net/http"

	"github.com/trentnix/hyperserver/pkg/services/session"
)

// cachedStore allows one read to populate the request cache. A second provider
// read fails, so tests cannot mistake a storage read for a cache hit.
type cachedStore struct {
	session.SessionStore
	value *session.Session
}

func (s *cachedStore) Get(*http.Request, string) (*session.Session, error) {
	value := s.value
	s.value = nil
	if value == nil {
		return nil, errors.New("unexpected second store read")
	}
	return value, nil
}

func setCachedSession(r *http.Request, value *session.Session) error {
	manager, err := session.NewSessionManager(r.Context(), map[string]string{"default": "test"}, map[string]session.StoreFactory{
		"test": func(context.Context) (session.SessionStore, error) {
			return &cachedStore{SessionStore: value.Store, value: value}, nil
		},
	})
	if err != nil {
		return err
	}
	*r = *session.AddSessionManagerToRequestContext(r, manager)
	_, err = session.Get(r, value.Name)
	return err
}

func getCachedSession(r *http.Request, name string) *session.Session {
	value, _ := session.Get(r, name)
	return value
}
