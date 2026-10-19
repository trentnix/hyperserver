package session

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

type managerTestStore struct {
	SessionStore
	close func() error
}

func (s *managerTestStore) Close() error { return s.close() }
func (s *managerTestStore) Get(_ *http.Request, name string) (*Session, error) {
	return NewSession(s, name), nil
}

type managerValueStore struct{ SessionStore }

func TestManagerAcceptsStoreWithoutAvailabilityFlag(t *testing.T) {
	// mockSessionStore implements only the four session operations.
	ready := &mockSessionStore{}
	m, err := NewSessionManager(t.Context(), map[string]string{"default": "custom"}, map[string]StoreFactory{
		"custom": func(context.Context) (SessionStore, error) { return ready, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Error(err)
		}
	})
	if store, err := m.Store("default"); err != nil || store != ready {
		t.Fatalf("store = %v, error = %v", store, err)
	}
}

func TestManagerAcceptsValueProvider(t *testing.T) {
	m, err := NewSessionManager(t.Context(), map[string]string{"default": "value"}, map[string]StoreFactory{
		"value": func(context.Context) (SessionStore, error) { return managerValueStore{}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Error(err)
		}
	})
	store, err := m.Store("default")
	if _, ok := store.(managerValueStore); err != nil || !ok {
		t.Fatalf("store = %T, error = %v", store, err)
	}
}

func TestManagerOwnsSelectedProviders(t *testing.T) {
	var events []string
	store := func(name string) StoreFactory {
		return func(context.Context) (SessionStore, error) {
			events = append(events, "open "+name)
			return &managerTestStore{close: func() error { events = append(events, "close "+name); return nil }}, nil
		}
	}
	types := map[string]string{"default": "B", "named": "a", "alias": "A"}
	factories := map[string]StoreFactory{"b": store("b"), "a": store("a"), "unused": store("unused")}
	m, err := NewSessionManager(t.Context(), types, factories)
	if err != nil {
		t.Fatal(err)
	}
	types["default"] = "unused"
	delete(factories, "a")
	r := AddSessionManagerToRequestContext(httptest.NewRequest(http.MethodGet, "/", nil), m)
	first, err := Get(r, "named")
	if err != nil {
		t.Fatal(err)
	}
	alias, err := m.Store("alias")
	if err != nil || first.Store != alias {
		t.Fatal("aliases did not reuse the initialized store", err)
	}
	fallback, err := m.Store("anything")
	if err != nil || fallback == alias {
		t.Fatal("default provider selection was lost", err)
	}
	for range 2 {
		if err := m.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if want := []string{"open a", "open b", "close b", "close a"}; !reflect.DeepEqual(events, want) {
		t.Fatalf("lifecycle = %v, want %v", events, want)
	}
}

func TestManagerRejectsInvalidSelectionsBeforeSetup(t *testing.T) {
	factory := func(context.Context) (SessionStore, error) {
		t.Fatal("invalid selections started a provider")
		return nil, nil
	}
	for _, tc := range []struct {
		name      string
		types     map[string]string
		factories map[string]StoreFactory
	}{
		{"unknown", map[string]string{"default": "a", "named": "unknown"}, map[string]StoreFactory{"a": factory}},
		{"empty selection", map[string]string{"default": ""}, map[string]StoreFactory{"a": factory}},
		{"duplicate provider", nil, map[string]StoreFactory{"a": factory, "A": factory}},
		{"nil factory", nil, map[string]StoreFactory{"a": nil}},
		{"empty provider", nil, map[string]StoreFactory{"": factory}},
		{"whitespace provider", nil, map[string]StoreFactory{" a": factory}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if m, err := NewSessionManager(t.Context(), tc.types, tc.factories); m != nil || err == nil {
				t.Fatalf("manager = %v, %v", m, err)
			}
		})
	}
}

func TestManagerRollsBackProviderFailures(t *testing.T) {
	failure, closeFailure := errors.New("setup failed"), errors.New("close failed")
	for _, reason := range []string{"error", "nil store", "typed-nil store", "canceled during setup", "canceled before setup"} {
		t.Run(reason, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var opened, closed []string
			factories := map[string]StoreFactory{}
			for _, name := range []string{"a", "b", "c"} {
				factories[name] = func(context.Context) (SessionStore, error) {
					opened = append(opened, name)
					if name == "b" {
						switch reason {
						case "error":
							return nil, failure
						case "nil store":
							return nil, nil
						case "typed-nil store":
							return (*managerTestStore)(nil), nil
						case "canceled during setup":
							cancel()
						}
					}
					return &managerTestStore{close: func() error { closed = append(closed, name); return closeFailure }}, nil
				}
			}
			if reason == "canceled before setup" {
				cancel()
			}
			m, err := NewSessionManager(ctx, map[string]string{"default": "a", "second": "b", "third": "c"}, factories)
			if err == nil || m != nil {
				t.Fatalf("manager = %v, %v", m, err)
			}
			if ctx.Err() != nil && !errors.Is(err, context.Canceled) {
				t.Fatal("lost cancellation", err)
			}
			if reason == "canceled before setup" {
				if len(opened) != 0 {
					t.Fatal("canceled setup initialized a provider")
				}
				return
			}
			if !reflect.DeepEqual(opened, []string{"a", "b"}) {
				t.Fatal("continued after failed setup", opened)
			}
			want := []string{"a"}
			if reason == "canceled during setup" {
				want = []string{"b", "a"}
			}
			if !reflect.DeepEqual(closed, want) || !errors.Is(err, closeFailure) {
				t.Fatalf("rollback = %v, %v", closed, err)
			}
			if reason == "error" && !errors.Is(err, failure) {
				t.Fatal("lost initialization error", err)
			}
			if strings.Contains(reason, "nil") && !strings.Contains(err.Error(), `session store "b" returned no instance`) {
				t.Fatal("lost invalid provider error", err)
			}
		})
	}
}
