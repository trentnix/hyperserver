package stores

import (
	"bytes"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	sessions "github.com/trentnix/hyperserver/pkg/services/session"
)

// taggedSerializer exercises an alternate wire format without another dependency.
// Legacy JSON is accepted only when the application explicitly enables it.
type taggedSerializer struct {
	legacy  bool
	failure error
}

func (s taggedSerializer) Encode(data map[string]any) ([]byte, error) {
	if s.failure != nil {
		return nil, s.failure
	}
	dataBytes, err := (sessions.JSONSerializer{}).Encode(data)
	return append([]byte("test-v1:"), dataBytes...), err
}

func (s taggedSerializer) Decode(data []byte) (map[string]any, error) {
	if s.failure != nil {
		return nil, s.failure
	}
	if rest, ok := bytes.CutPrefix(data, []byte("test-v1:")); ok {
		data = rest
	} else if !s.legacy {
		return nil, errors.New("unsupported session format")
	}
	return (sessions.JSONSerializer{}).Decode(data)
}

func serializerStore(t *testing.T, kind string, serializer sessions.Serializer) sessions.SessionStore {
	t.Helper()
	if kind == "cookie" {
		store := setupCookieStore(t)
		store.Serializer = serializer
		return store
	}
	store := revocationStore(t)
	store.Serializer = serializer
	return store
}

func requestWithSessionCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Request {
	t.Helper()
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %v", cookies)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(cookies[0])
	return r
}

func TestStoresUseSelectedSerializer(t *testing.T) {
	for _, kind := range []string{"cookie", "sqlite"} {
		t.Run(kind, func(t *testing.T) {
			store := serializerStore(t, kind, taggedSerializer{})
			s, r := savedSession(t, store)
			s.Data["version"] = int64(math.MaxInt64)
			s.Data["messages"] = []map[string]string{{"text": "saved"}}
			w := httptest.NewRecorder()
			if err := s.Save(w, r); err != nil {
				t.Fatal(err)
			}
			r = requestWithSessionCookie(t, w)
			loaded, err := store.Get(r, s.Name)
			if err != nil || loaded.ID != s.ID || loaded.Data["identity"] != "account" {
				t.Fatalf("custom round trip = %+v, %v", loaded, err)
			}
			var version int64
			var messages []map[string]string
			if err := loaded.DecodeValue("version", &version); err != nil || version != math.MaxInt64 {
				t.Fatalf("version = %d, %v", version, err)
			}
			if err := loaded.DecodeValue("messages", &messages); err != nil || len(messages) != 1 || messages[0]["text"] != "saved" {
				t.Fatalf("messages = %v, %v", messages, err)
			}

			if kind == "sqlite" {
				oldRequest := r
				w = httptest.NewRecorder()
				if err := loaded.Rotate(w, r, map[string]any{"identity": "rotated"}); err != nil {
					t.Fatal(err)
				}
				r = requestWithSessionCookie(t, w)
				rotated, err := store.Get(r, s.Name)
				if err != nil || rotated.ID == s.ID || rotated.Data["identity"] != "rotated" {
					t.Fatalf("custom rotation = %+v, %v", rotated, err)
				}
				if revoked, err := store.Get(oldRequest, s.Name); err != nil || !revoked.IsNew {
					t.Fatalf("rotation retained previous ID: %+v, %v", revoked, err)
				}
			}
		})
	}
}

func TestSerializerReplacementCompatibility(t *testing.T) {
	for _, kind := range []string{"cookie", "sqlite"} {
		t.Run(kind, func(t *testing.T) {
			original := serializerStore(t, kind, nil)
			s, r := savedSession(t, original)
			// Separate provider instances share only storage and signing settings.
			// Runtime serializer configuration stays unchanged.
			var strict, compatible sessions.SessionStore
			switch store := original.(type) {
			case *CookieStore:
				one, two := *store, *store
				one.Serializer, two.Serializer = taggedSerializer{}, taggedSerializer{legacy: true}
				strict, compatible = &one, &two
			case *SQLiteStore:
				one, two := *store, *store
				one.Serializer, two.Serializer = taggedSerializer{}, taggedSerializer{legacy: true}
				strict, compatible = &one, &two
			}
			if loaded, err := strict.Get(r, s.Name); loaded != nil || err == nil {
				t.Fatalf("incompatible format accepted: %+v, %v", loaded, err)
			}
			loaded, err := compatible.Get(r, s.Name)
			if err != nil || loaded.ID != s.ID || loaded.Data["identity"] != "account" {
				t.Fatalf("legacy session lost: %+v, %v", loaded, err)
			}
			w := httptest.NewRecorder()
			if err := loaded.Save(w, r); err != nil {
				t.Fatal(err)
			}
			r = requestWithSessionCookie(t, w)
			if loaded, err := strict.Get(r, s.Name); err != nil || loaded.ID != s.ID || loaded.Data["identity"] != "account" {
				t.Fatalf("replacement did not write its own format: %+v, %v", loaded, err)
			}
			if loaded, err := original.Get(r, s.Name); loaded != nil || err == nil {
				t.Fatalf("default decoder accepted incompatible data: %+v, %v", loaded, err)
			}
		})
	}
}

func TestSerializerFailurePreservesStoredSession(t *testing.T) {
	failure := errors.New("serialization failed")
	for _, kind := range []string{"cookie", "sqlite"} {
		t.Run(kind, func(t *testing.T) {
			original := serializerStore(t, kind, nil)
			s, r := savedSession(t, original)
			var failing sessions.SessionStore
			switch store := original.(type) {
			case *CookieStore:
				copy := *store
				copy.Serializer = taggedSerializer{failure: failure}
				failing = &copy
			case *SQLiteStore:
				copy := *store
				copy.Serializer = taggedSerializer{failure: failure}
				failing = &copy
			}
			w := httptest.NewRecorder()
			if err := failing.Save(w, r, s); !errors.Is(err, failure) || len(w.Result().Cookies()) != 0 {
				t.Fatalf("failed serialization issued a cookie: %v", err)
			}
			if rotator, ok := failing.(sessions.RotatingStore); ok {
				if next, err := rotator.Rotate(w, r, s, map[string]any{"identity": "changed"}); next != nil || !errors.Is(err, failure) || len(w.Result().Cookies()) != 0 {
					t.Fatalf("failed rotation = %+v, %v", next, err)
				}
			}
			if loaded, err := failing.Get(r, s.Name); loaded != nil || !errors.Is(err, failure) {
				t.Fatalf("decode failure treated as missing session: %+v, %v", loaded, err)
			}
			if loaded, err := original.Get(r, s.Name); err != nil || loaded.ID != s.ID || loaded.Data["identity"] != "account" {
				t.Fatalf("serializer failure changed storage: %+v, %v", loaded, err)
			}
		})
	}
}

func TestSelectedSerializerConcurrentRequests(t *testing.T) {
	for _, kind := range []string{"cookie", "sqlite"} {
		t.Run(kind, func(t *testing.T) {
			store := serializerStore(t, kind, taggedSerializer{})
			if sqlite, ok := store.(*SQLiteStore); ok {
				sqlite.db.SetMaxOpenConns(1)
			}
			var workers sync.WaitGroup
			for range 8 {
				workers.Go(func() {
					s := sessions.NewSession(store, "concurrent")
					s.Data["identity"] = "account"
					r := httptest.NewRequest(http.MethodGet, "/", nil)
					w := httptest.NewRecorder()
					if err := s.Save(w, r); err != nil {
						t.Error(err)
						return
					}
					for _, cookie := range w.Result().Cookies() {
						r.AddCookie(cookie)
					}
					loaded, err := store.Get(r, s.Name)
					if err != nil || loaded.ID != s.ID || loaded.Data["identity"] != "account" {
						t.Errorf("concurrent round trip = %+v, %v", loaded, err)
					}
				})
			}
			workers.Wait()
		})
	}
}

func TestSelectedSerializerSizeLimits(t *testing.T) {
	for _, kind := range []string{"cookie", "sqlite"} {
		t.Run(kind, func(t *testing.T) {
			store := serializerStore(t, kind, taggedSerializer{})
			s, r := savedSession(t, store)
			s.Data["large"] = strings.Repeat("a", sessions.MaxSessionDataSize)
			w := httptest.NewRecorder()
			if err := s.Save(w, r); !errors.Is(err, sessions.ErrSessionTooLarge) || len(w.Result().Cookies()) != 0 {
				t.Fatalf("oversized custom encoding = %v", err)
			}
			if rotator, ok := store.(sessions.RotatingStore); ok {
				if next, err := rotator.Rotate(w, r, s, s.Data); next != nil || !errors.Is(err, sessions.ErrSessionTooLarge) || len(w.Result().Cookies()) != 0 {
					t.Fatalf("oversized custom rotation = %+v, %v", next, err)
				}
			} else {
				s.Data["large"] = strings.Repeat("a", sessions.MaxCookieSize)
				if err := s.Save(w, r); !errors.Is(err, sessions.ErrSessionCookieTooLarge) || len(w.Result().Cookies()) != 0 {
					t.Fatalf("oversized custom cookie = %v", err)
				}
			}
			if loaded, err := store.Get(r, s.Name); err != nil || len(loaded.Data) != 1 || loaded.Data["identity"] != "account" {
				t.Fatalf("size rejection changed stored data: %+v, %v", loaded, err)
			}
		})
	}
}
