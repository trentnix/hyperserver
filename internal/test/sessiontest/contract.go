// Package sessiontest checks the revocable session-storage contract.
// It is test support, not an application storage implementation.
package sessiontest

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/trentnix/hyperserver/pkg/services/session"
)

// Run checks a server-side store that implements SessionStore and RotatingStore.
// Each factory call must return isolated, enabled storage with a finite lifetime
// and register its cleanup with t. Backend-specific failures, encoded-size limits,
// and expiry boundaries remain in provider tests, using their serializer and clock.
func Run(t *testing.T, open func(*testing.T) session.SessionStore) {
	t.Helper()
	t.Run("round trip and independent reads", func(t *testing.T) {
		store := open(t)
		if !store.IsEnabled() {
			t.Fatal("test store is disabled")
		}
		missing, err := store.Get(request(), "account")
		if err != nil || missing == nil || !missing.IsNew || len(missing.Data) != 0 {
			t.Fatalf("missing session = %+v, %v", missing, err)
		}
		original, r := saved(t, store)
		first := load(t, store, r, original.Name)
		second := load(t, store, r, original.Name)
		if first.ID != original.ID || first.IsNew || first.Store == nil || !first.ExpiresAt.Equal(second.ExpiresAt) {
			t.Fatal("read lost stored identity or expiration")
		}
		first.Data["value"] = "changed"
		if second.Data["value"] != "original" {
			t.Fatal("reads share mutable data")
		}
		w := httptest.NewRecorder()
		if err := store.Save(w, r, first); err != nil {
			t.Fatal(err)
		}
		updated := load(t, store, cookieRequest(t, w), original.Name)
		if updated.Data["value"] != "changed" || !updated.ExpiresAt.Equal(second.ExpiresAt) {
			t.Fatal("save lost data or extended expiry")
		}
	})

	t.Run("structured values", func(t *testing.T) {
		store := open(t)
		s, r := saved(t, store)
		s.Data["version"] = int64(math.MaxInt64)
		s.Data["messages"] = []map[string]string{{"text": "saved <世界>"}}
		w := httptest.NewRecorder()
		if err := store.Save(w, r, s); err != nil {
			t.Fatal(err)
		}
		loaded := load(t, store, cookieRequest(t, w), s.Name)
		var version int64
		var messages []map[string]string
		if err := loaded.DecodeValue("version", &version); err != nil || version != math.MaxInt64 {
			t.Fatalf("version = %d, %v", version, err)
		}
		if err := loaded.DecodeValue("messages", &messages); err != nil || len(messages) != 1 || messages[0]["text"] != "saved <世界>" {
			t.Fatalf("messages = %+v, %v", messages, err)
		}
	})

	t.Run("missing record and invalid cookie", func(t *testing.T) {
		store := open(t)
		s, r := saved(t, store)
		if err := store.End(httptest.NewRecorder(), r, s); err != nil {
			t.Fatal(err)
		}
		missing := load(t, store, r, s.Name)
		if !missing.IsNew || missing.ID == s.ID || len(missing.Data) != 0 {
			t.Fatal("missing record restored identity")
		}
		r = request()
		r.AddCookie(&http.Cookie{Name: s.Name, Value: "not-a-valid-session-token"})
		if got, err := store.Get(r, s.Name); got != nil || err == nil {
			t.Fatalf("invalid cookie = %+v, %v", got, err)
		}
	})

	t.Run("duplicate ID", func(t *testing.T) {
		store := open(t)
		s, r := saved(t, store)
		duplicate := session.NewSession(store, s.Name)
		duplicate.ID = s.ID
		duplicate.Data["value"] = "replacement"
		w := httptest.NewRecorder()
		if err := store.Save(w, r, duplicate); err == nil {
			t.Fatal("duplicate ID replaced an existing session")
		}
		noCookie(t, w)
		unchanged(t, store, r, s)
	})

	t.Run("revocation cannot be undone", func(t *testing.T) {
		store := open(t)
		s, r := saved(t, store)
		other, otherRequest := saved(t, store)
		stale := load(t, store, r, s.Name)
		for range 2 {
			w := httptest.NewRecorder()
			if err := store.End(w, r, s); err != nil {
				t.Fatal(err)
			}
			cookies := w.Result().Cookies()
			if len(cookies) != 1 || cookies[0].MaxAge != -1 {
				t.Fatal("End did not expire the cookie")
			}
		}
		w := httptest.NewRecorder()
		if err := store.Save(w, r, stale); err == nil {
			t.Fatal("stale save recreated a revoked session")
		}
		noCookie(t, w)
		if next, err := rotating(t, store).Rotate(w, r, stale, stale.Data); next != nil || err == nil {
			t.Fatalf("revoked session rotated: %+v, %v", next, err)
		}
		noCookie(t, w)
		unchanged(t, store, otherRequest, other)
	})

	t.Run("rotation and replay", func(t *testing.T) {
		store := open(t)
		old, r := saved(t, store)
		w := httptest.NewRecorder()
		next, err := rotating(t, store).Rotate(w, r, old, map[string]any{"value": "rotated"})
		if err != nil || next == nil || next.ID == old.ID || next.IsNew {
			t.Fatalf("rotation = %+v, %v", next, err)
		}
		loaded := load(t, store, cookieRequest(t, w), old.Name)
		if loaded.ID != next.ID || loaded.Data["value"] != "rotated" {
			t.Fatal("replacement cookie cannot load the new data")
		}
		if replay := load(t, store, r, old.Name); !replay.IsNew || replay.ID == old.ID {
			t.Fatal("old cookie restored a rotated session")
		}
	})

	t.Run("concurrent rotation", func(t *testing.T) {
		store := open(t)
		original, r := saved(t, store)
		rotator := rotating(t, store)
		type result struct {
			next   *session.Session
			writer *httptest.ResponseRecorder
			err    error
		}
		results := make(chan result, 2)
		start := make(chan struct{})
		for range 2 {
			previous := load(t, store, r, original.Name)
			go func() {
				<-start
				w := httptest.NewRecorder()
				next, err := rotator.Rotate(w, r.Clone(r.Context()), previous, map[string]any{"value": "rotated"})
				results <- result{next, w, err}
			}()
		}
		close(start)
		successes := 0
		completed := []result{<-results, <-results}
		for _, got := range completed {
			if got.err != nil {
				noCookie(t, got.writer)
				if got.next != nil {
					t.Error("failed rotation returned a session")
				}
				continue
			}
			successes++
			if got.next == nil {
				t.Error("successful rotation returned no session")
				continue
			}
			loaded := load(t, store, cookieRequest(t, got.writer), original.Name)
			if loaded.ID != got.next.ID || loaded.Data["value"] != "rotated" {
				t.Error("winning rotation was not persisted")
			}
		}
		if successes != 1 {
			t.Fatalf("successful rotations = %d, want 1", successes)
		}
	})

	for _, operation := range []string{"read", "save", "rotate", "end"} {
		t.Run("canceled "+operation, func(t *testing.T) {
			store := open(t)
			s, r := saved(t, store)
			ctx, cancel := context.WithCancel(r.Context())
			cancel()
			canceled := r.WithContext(ctx)
			w := httptest.NewRecorder()
			var err error
			var result *session.Session
			switch operation {
			case "read":
				result, err = store.Get(canceled, s.Name)
			case "save":
				err = store.Save(w, canceled, s)
			case "rotate":
				result, err = rotating(t, store).Rotate(w, canceled, s, s.Data)
			case "end":
				err = store.End(w, canceled, s)
			}
			if result != nil || !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled %s = %v", operation, err)
			}
			noCookie(t, w)
			unchanged(t, store, r, s)
		})
	}

	for _, operation := range []string{"save", "rotate"} {
		t.Run(operation+" rejects invalid data", func(t *testing.T) {
			store := open(t)
			s, r := saved(t, store)
			w := httptest.NewRecorder()
			var err error
			if operation == "save" {
				s.Data["bad"] = make(chan int)
				err = store.Save(w, r, s)
			} else {
				_, err = rotating(t, store).Rotate(w, r, s, map[string]any{"bad": make(chan int)})
			}
			if err == nil {
				t.Fatal("invalid data was accepted")
			}
			noCookie(t, w)
			unchanged(t, store, r, s)
		})
	}
}

func request() *http.Request {
	return httptest.NewRequest(http.MethodGet, "http://example.invalid/", nil)
}

func cookieRequest(t *testing.T, w *httptest.ResponseRecorder) *http.Request {
	t.Helper()
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %v, want one", cookies)
	}
	r := request()
	r.AddCookie(cookies[0])
	return r
}

func saved(t *testing.T, store session.SessionStore) (*session.Session, *http.Request) {
	t.Helper()
	r := request()
	s, err := store.New(r, "account")
	if err != nil || s == nil || !s.IsNew || s.ID == "" {
		t.Fatalf("new session = %+v, %v", s, err)
	}
	s.Data["value"] = "original"
	w := httptest.NewRecorder()
	if err := store.Save(w, r, s); err != nil {
		t.Fatal(err)
	}
	return s, cookieRequest(t, w)
}

func load(t *testing.T, store session.SessionStore, r *http.Request, name string) *session.Session {
	t.Helper()
	s, err := store.Get(r, name)
	if err != nil || s == nil {
		t.Fatalf("load session = %+v, %v", s, err)
	}
	return s
}

func rotating(t *testing.T, store session.SessionStore) session.RotatingStore {
	t.Helper()
	rotator, ok := store.(session.RotatingStore)
	if !ok {
		t.Fatal("store does not support revocable rotation")
	}
	return rotator
}

func noCookie(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if len(w.Header().Values("Set-Cookie")) != 0 {
		t.Fatal("failed operation issued a cookie")
	}
}

func unchanged(t *testing.T, store session.SessionStore, r *http.Request, original *session.Session) {
	t.Helper()
	loaded := load(t, store, r, original.Name)
	if loaded.ID != original.ID || len(loaded.Data) != 1 || loaded.Data["value"] != "original" {
		t.Fatal("operation changed stored data")
	}
}
