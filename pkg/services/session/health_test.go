package session

import (
	"context"
	"errors"
	"testing"
)

type healthStore struct {
	SessionStore
	calls int
	err   error
}

func (s *healthStore) CheckHealth(ctx context.Context) error {
	s.calls++
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return s.err
}

func TestHealthChecksOnlySelectedStoresOnce(t *testing.T) {
	selected := &healthStore{}
	unused := &healthStore{err: errors.New("unused unavailable")}
	m, err := NewSessionManager(t.Context(), map[string]string{"default": "active", "other": "active"}, map[string]StoreFactory{
		"active": func(context.Context) (SessionStore, error) { return selected, nil },
		"unused": func(context.Context) (SessionStore, error) { t.Fatal("initialized unused store"); return unused, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.CheckHealth(t.Context()); err != nil {
		t.Fatal(err)
	}
	if selected.calls != 1 || unused.calls != 0 {
		t.Fatal("incorrect probe selection")
	}
	selected.err = errors.New("offline")
	if err := m.CheckHealth(t.Context()); !errors.Is(err, selected.err) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := m.CheckHealth(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
