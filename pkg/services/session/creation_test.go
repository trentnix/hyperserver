package session

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type creationStore struct {
	SessionStore
	create func(*http.Request, string) (*Session, error)
	end    func() error
}

func (*creationStore) Get(*http.Request, string) (*Session, error) { return nil, nil }

func (s *creationStore) New(r *http.Request, name string) (*Session, error)     { return s.create(r, name) }
func (s *creationStore) End(http.ResponseWriter, *http.Request, *Session) error { return s.end() }

func TestNewDelegatesToStore(t *testing.T) {
	failure := errors.New("creation failed")
	for _, outcome := range []string{"success", "error", "nil"} {
		t.Run(outcome, func(t *testing.T) {
			store := &creationStore{}
			manager := &SessionManager{Types: map[string]string{"default": "custom"}, stores: map[string]SessionStore{"custom": store}}
			r := AddSessionManagerToRequestContext(httptest.NewRequest("GET", "/", nil), manager)
			old := NewSession(store, "session")
			if err := setCachedSession(r, old); err != nil {
				t.Fatal(err)
			}
			next := NewSession(store, "session")
			next.Data["provider-default"] = true
			calls := 0
			store.create = func(got *http.Request, name string) (*Session, error) {
				calls++
				if got != r || name != "session" {
					t.Fatal("creation lost request or name")
				}
				if outcome == "error" {
					return next, failure
				}
				if outcome == "nil" {
					return nil, nil
				}
				return next, nil
			}
			got, err := New(r, "session")
			if calls != 1 {
				t.Fatal("store.New was not called exactly once")
			}
			if outcome == "success" {
				if err != nil || got != next || getCachedSession(r, "session") != next {
					t.Fatal("provider result was not cached", err)
				}
			} else {
				if err == nil || got != nil || getCachedSession(r, "session") != old {
					t.Fatal("failed creation replaced the cached session", err)
				}
				if outcome == "error" && !errors.Is(err, failure) {
					t.Fatal("lost provider error", err)
				}
			}
		})
	}
}

func TestGetUsesProviderCreationAndRetriesFailures(t *testing.T) {
	store := &creationStore{}
	manager := &SessionManager{Types: map[string]string{"default": "custom"}, stores: map[string]SessionStore{"custom": store}}
	r := AddSessionManagerToRequestContext(httptest.NewRequest("GET", "/", nil), manager)
	failure := errors.New("creation failed")
	calls := 0
	store.create = func(*http.Request, string) (*Session, error) {
		calls++
		if calls == 1 {
			return nil, failure
		}
		return NewSession(store, "session"), nil
	}
	if got, err := Get(r, "session"); got != nil || !errors.Is(err, failure) || getCachedSession(r, "session") != nil {
		t.Fatal("failed creation was cached", err)
	}
	created, err := Get(r, "session")
	if err != nil || created == nil {
		t.Fatal("creation could not be retried", err)
	}
	if cached, err := Get(r, "session"); err != nil || cached != created || calls != 2 {
		t.Fatal("successful creation was not cached", err)
	}
}

func TestEndCreatesReplacementBeforeRevocation(t *testing.T) {
	failure := errors.New("provider failure")
	for _, outcome := range []string{"success", "creation error", "nil replacement", "revocation error"} {
		t.Run(outcome, func(t *testing.T) {
			store := &creationStore{}
			r := httptest.NewRequest("GET", "/", nil)
			current := NewSession(store, "session")
			current.Data["secret"] = "old"
			oldID, oldData := current.ID, current.Data
			if err := setCachedSession(r, current); err != nil {
				t.Fatal(err)
			}
			next := NewSession(store, "session")
			next.Data["provider-default"] = true
			created, ended := false, false
			store.create = func(got *http.Request, name string) (*Session, error) {
				created = true
				if got != r || name != "session" {
					t.Fatal("wrong replacement inputs")
				}
				if outcome == "creation error" {
					return nil, failure
				}
				if outcome == "nil replacement" {
					return nil, nil
				}
				return next, nil
			}
			store.end = func() error {
				if !created {
					t.Fatal("revoked before creating replacement")
				}
				ended = true
				if outcome == "revocation error" {
					return failure
				}
				return nil
			}
			err := current.End(httptest.NewRecorder(), r)
			if outcome == "success" {
				if err != nil || !ended || current.ID != next.ID || current.Data["provider-default"] != true || len(oldData) != 0 {
					t.Fatal("replacement was not published", err)
				}
			} else {
				if err == nil || current.ID != oldID || oldData["secret"] != "old" {
					t.Fatal("failure changed the session", err)
				}
				if ended != (outcome == "revocation error") {
					t.Fatal("creation failure attempted revocation")
				}
			}
			if getCachedSession(r, "session") != current {
				t.Fatal("lost cached session")
			}
		})
	}
}
