package sqlite

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sessions "github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/session/cookie"
	"github.com/trentnix/hyperserver/pkg/services/session/internal/sessioncookie"
)

// fixedPayloadSerializer seeds exact stored formats without using the current encoder.
type fixedPayloadSerializer string

func (s fixedPayloadSerializer) Encode(map[string]any) ([]byte, error) { return []byte(s), nil }
func (s fixedPayloadSerializer) Decode(data []byte) (map[string]any, error) {
	return (sessions.JSONSerializer{}).Decode(data)
}

func storedPayloadFixture(t *testing.T, kind, raw string) (sessions.SessionStore, *sessions.Session, *http.Request) {
	t.Helper()
	writer := serializerStore(t, kind, fixedPayloadSerializer(raw))
	s, r := savedSession(t, writer)
	// Use a separate instance so no live serializer configuration changes.
	switch original := writer.(type) {
	case *cookie.CookieStore:
		reader := *original
		reader.Serializer = nil
		return &reader, s, r
	case *SQLiteStore:
		reader := *original
		reader.Serializer = nil
		return &reader, s, r
	default:
		t.Fatalf("unexpected store %T", writer)
		return nil, nil, nil
	}
}

func storedPayload(t *testing.T, store sessions.SessionStore, r *http.Request, name string) string {
	t.Helper()
	requestCookie, err := r.Cookie(name)
	if err != nil {
		t.Fatal(err)
	}
	var encoded string
	switch store := store.(type) {
	case *cookie.CookieStore:
		claims, err := sessioncookie.Parse(requestCookie.Value, store.JwtKey, name)
		if err != nil {
			t.Fatal(err)
		}
		encoded = claims.Value
	case *SQLiteStore:
		claims, err := sessioncookie.Parse(requestCookie.Value, store.JwtKey, name)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.db.QueryRowContext(t.Context(), `SELECT session FROM sessions WHERE id = ?`, claims.ID).Scan(&encoded); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unexpected store %T", store)
	}
	decoded, err := base64.URLEncoding.Strict().DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return string(decoded)
}

func TestStoresReadAndRewriteLegacyPayloads(t *testing.T) {
	const legacy = `{"version":99,"data":{"text":"saved"},"n":9223372036854775807}`
	const current = `[1,{"data":{"text":"saved"},"n":9223372036854775807,"version":99}]`
	for _, kind := range []string{"cookie", "sqlite"} {
		t.Run(kind, func(t *testing.T) {
			store, original, r := storedPayloadFixture(t, kind, legacy)
			loaded, err := store.Get(r, original.Name)
			if err != nil || loaded == nil || loaded.IsNew || loaded.ID != original.ID || loaded.Data["n"] != json.Number("9223372036854775807") {
				t.Fatalf("legacy read = %+v, %v", loaded, err)
			}
			if got := storedPayload(t, store, r, original.Name); got != legacy {
				t.Fatalf("read changed stored payload: %s", got)
			}
			expiration := loaded.ExpiresAt
			w := httptest.NewRecorder()
			if err := loaded.Save(w, r); err != nil {
				t.Fatal(err)
			}
			r = requestWithSessionCookie(t, w)
			if got := storedPayload(t, store, r, original.Name); got != current {
				t.Fatalf("save did not write version 1: %s", got)
			}
			reloaded, err := store.Get(r, original.Name)
			if err != nil || reloaded == nil || reloaded.ID != original.ID || !reloaded.ExpiresAt.Equal(expiration) || reloaded.Data["version"] != json.Number("99") {
				t.Fatalf("rewrite changed identity, expiry, or data: %+v, %v", reloaded, err)
			}
		})
	}
}

func TestStoresRejectInvalidPayloadVersions(t *testing.T) {
	for _, kind := range []string{"cookie", "sqlite"} {
		for _, raw := range []string{`[2,{}]`, `[null,{}]`, `["1",{}]`, `[1,null]`, `[1,{},{}]`, `[1,{}] garbage`} {
			t.Run(kind+"/"+raw, func(t *testing.T) {
				store, original, r := storedPayloadFixture(t, kind, raw)
				for range 2 {
					if loaded, err := store.Get(r, original.Name); loaded != nil || err == nil {
						t.Fatalf("invalid payload accepted: %+v, %v", loaded, err)
					}
				}
				if got := storedPayload(t, store, r, original.Name); got != raw {
					t.Fatalf("failed read changed stored payload: %s", got)
				}
			})
		}
	}
}

func TestFailedPayloadRewritePreservesLegacyData(t *testing.T) {
	const legacy = `{"identity":"account"}`
	for _, kind := range []string{"cookie", "sqlite"} {
		t.Run(kind, func(t *testing.T) {
			store, original, r := storedPayloadFixture(t, kind, legacy)
			loaded, err := store.Get(r, original.Name)
			if err != nil {
				t.Fatal(err)
			}
			loaded.Data["unsupported"] = make(chan int)
			w := httptest.NewRecorder()
			if err := loaded.Save(w, r); err == nil || len(w.Header().Values("Set-Cookie")) != 0 {
				t.Fatalf("failed rewrite issued a cookie: %v", err)
			}
			if got := storedPayload(t, store, r, original.Name); got != legacy {
				t.Fatalf("failed encoding changed stored payload: %s", got)
			}
		})
	}

	t.Run("SQLite write failure", func(t *testing.T) {
		store, original, r := storedPayloadFixture(t, "sqlite", legacy)
		sqlite := store.(*SQLiteStore)
		if _, err := sqlite.db.ExecContext(t.Context(), `CREATE TRIGGER reject_rewrite BEFORE UPDATE ON sessions BEGIN SELECT RAISE(ABORT, 'write blocked'); END`); err != nil {
			t.Fatal(err)
		}
		loaded, err := store.Get(r, original.Name)
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		if err := loaded.Save(w, r); err == nil || !strings.Contains(err.Error(), "write blocked") || len(w.Header().Values("Set-Cookie")) != 0 {
			t.Fatalf("failed database rewrite = %v", err)
		}
		if got := storedPayload(t, store, r, original.Name); got != legacy {
			t.Fatal("failed database rewrite changed the original payload")
		}
	})
}

func TestLegacyPayloadRewriteSizeLimit(t *testing.T) {
	legacy := `{"x":"` + strings.Repeat("a", sessions.MaxSessionDataSize-8) + `"}`
	store, original, r := storedPayloadFixture(t, "sqlite", legacy)
	loaded, err := store.Get(r, original.Name)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	if err := loaded.Save(w, r); !errors.Is(err, sessions.ErrSessionTooLarge) || len(w.Header().Values("Set-Cookie")) != 0 {
		t.Fatalf("version overhead not included in size limit: %v", err)
	}
	if got := storedPayload(t, store, r, original.Name); got != legacy {
		t.Fatal("oversized rewrite changed stored data")
	}
}

func TestLegacyCookieRewriteSizeLimit(t *testing.T) {
	// Find a legacy cookie that fits before the version wrapper is added.
	store := setupCookieStore(t)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	s := sessions.NewSession(store, "boundary-session")
	low, high := 0, sessions.MaxCookieSize
	for low < high {
		size := (low + high + 1) / 2
		store.Serializer = fixedPayloadSerializer(`{"x":"` + strings.Repeat("a", size) + `"}`)
		err := store.Save(httptest.NewRecorder(), r, s)
		if errors.Is(err, sessions.ErrSessionCookieTooLarge) {
			high = size - 1
		} else if err != nil {
			t.Fatal(err)
		} else {
			low = size
		}
	}
	legacy := `{"x":"` + strings.Repeat("a", low) + `"}`
	store.Serializer = fixedPayloadSerializer(legacy)
	w := httptest.NewRecorder()
	if err := store.Save(w, r, s); err != nil {
		t.Fatal(err)
	}
	r = requestWithSessionCookie(t, w)
	reader := *store
	reader.Serializer = nil
	loaded, err := reader.Get(r, s.Name)
	if err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	if err := loaded.Save(w, r); !errors.Is(err, sessions.ErrSessionCookieTooLarge) || len(w.Header().Values("Set-Cookie")) != 0 {
		t.Fatalf("oversized versioned cookie rewrite = %v", err)
	}
	if got := storedPayload(t, &reader, r, s.Name); got != legacy {
		t.Fatal("oversized rewrite changed the original cookie")
	}
}
