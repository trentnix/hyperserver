package session

import (
	"bytes"
	"encoding/base64"
	"encoding/gob"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSessionJSONRoundTrip(t *testing.T) {
	type message struct {
		Text string `json:"text"`
	}
	original := &Session{Data: map[string]any{
		"text": "hello <世界>", "version": int64(math.MaxInt64), "counter": 3,
		"messages": []message{{Text: "saved"}}, "enabled": true,
	}}
	encoded, err := original.EncodedData()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := loadSession("id", "name", time.Now().Add(time.Hour), nil, encoded)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Data["text"] != original.Data["text"] || loaded.Data["enabled"] != true {
		t.Fatalf("changed values: %v", loaded.Data)
	}
	if number, ok := loaded.Data["version"].(json.Number); !ok || number.String() != "9223372036854775807" {
		t.Fatalf("integer precision lost: %v", loaded.Data["version"])
	}
	for _, s := range []*Session{original, loaded} {
		var version int64
		var counter int
		var messages []message
		if err := s.DecodeValue("version", &version); err != nil || version != math.MaxInt64 {
			t.Fatalf("version = %d, %v", version, err)
		}
		if err := s.DecodeValue("counter", &counter); err != nil || counter != 3 {
			t.Fatalf("counter = %d, %v", counter, err)
		}
		if err := s.DecodeValue("messages", &messages); err != nil || len(messages) != 1 || messages[0].Text != "saved" {
			t.Fatalf("messages = %v, %v", messages, err)
		}
		if err := s.DecodeValue("absent", &counter); err != nil || counter != 3 {
			t.Fatalf("missing key changed target: %d, %v", counter, err)
		}
		if err := s.DecodeValue("text", &counter); err == nil {
			t.Fatal("wrong value type accepted")
		}
	}
}

func TestSessionJSONSizeLimits(t *testing.T) {
	for _, size := range []int{MaxSessionDataSize - 1, MaxSessionDataSize, MaxSessionDataSize + 1} {
		// The object syntax contributes eight bytes.
		value := strings.Repeat("a", size-8)
		s := &Session{Data: map[string]any{"x": value}}
		encoded, err := s.EncodedData()
		if size > MaxSessionDataSize {
			if !errors.Is(err, ErrSessionTooLarge) || encoded != "" {
				t.Fatalf("oversized encoding = %q, %v", encoded, err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
		raw := base64.URLEncoding.EncodeToString([]byte(`{"x":"` + value + `"}`))
		loaded, err := loadSession("id", "name", time.Now(), nil, raw)
		if size > MaxSessionDataSize {
			if !errors.Is(err, ErrSessionTooLarge) || loaded != nil {
				t.Fatalf("oversized decoding accepted: %v", err)
			}
		} else if err != nil || loaded.Data["x"] != value {
			t.Fatalf("bounded decoding failed: %v", err)
		}
	}
	if _, err := loadSession("id", "name", time.Now(), nil, strings.Repeat("!", maxEncodedSessionSize+1)); !errors.Is(err, ErrSessionTooLarge) {
		t.Fatalf("encoded size was not checked first: %v", err)
	}
}

func TestSessionJSONRejectsInvalidData(t *testing.T) {
	var legacy bytes.Buffer
	if err := gob.NewEncoder(&legacy).Encode(map[string]any{"x": "legacy"}); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"", "null", "[]", "1", `"text"`, `{"x":`, `{} {}`, `{} garbage`, legacy.String()} {
		encoded := base64.URLEncoding.EncodeToString([]byte(raw))
		if loaded, err := loadSession("id", "name", time.Now(), nil, encoded); err == nil || loaded != nil {
			t.Fatalf("invalid data accepted: %.40q", raw)
		}
	}
	if _, err := loadSession("id", "name", time.Now(), nil, "!invalid-base64!"); err == nil {
		t.Fatal("invalid base64 accepted")
	}
	for _, value := range []any{make(chan int), math.Inf(1), math.NaN()} {
		if _, err := (&Session{Data: map[string]any{"x": value}}).EncodedData(); err == nil {
			t.Fatal("unsupported JSON value accepted")
		}
	}
	encoded, err := (&Session{}).EncodedData()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := loadSession("id", "name", time.Now(), nil, encoded)
	if err != nil || loaded.Data == nil || len(loaded.Data) != 0 {
		t.Fatalf("empty session = %+v, %v", loaded, err)
	}
}

func TestSessionJSONNesting(t *testing.T) {
	for _, tc := range []struct {
		name  string
		depth int
	}{
		{"within limit", 100},
		{"excessive nesting", 10001},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Balanced arrays inside an object isolate nesting from syntax and root-type errors.
			raw := `{"nested":` + strings.Repeat("[", tc.depth) + "0" + strings.Repeat("]", tc.depth) + "}"
			if len(raw) > MaxSessionDataSize {
				t.Fatal("fixture exceeds the byte limit instead of testing nesting")
			}
			encoded := base64.URLEncoding.EncodeToString([]byte(raw))
			loaded, err := loadSession("id", "name", time.Now(), nil, encoded)
			if tc.depth == 100 {
				if err != nil || loaded == nil {
					t.Fatalf("valid nested object rejected: %v", err)
				}
				return
			}
			var syntaxErr *json.SyntaxError
			if loaded != nil || !errors.As(err, &syntaxErr) || !strings.Contains(syntaxErr.Error(), "exceeded max depth") {
				t.Fatalf("expected nesting-limit error, got %v", err)
			}
		})
	}
}

func TestSessionCookieSizeLimits(t *testing.T) {
	cookie := &http.Cookie{Name: "session", Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode}
	for _, size := range []int{MaxCookieSize - 1, MaxCookieSize, MaxCookieSize + 1} {
		cookie.Value = ""
		cookie.Value = strings.Repeat("a", size-len(cookie.String()))
		err := validateSessionCookie(cookie)
		if size > MaxCookieSize && !errors.Is(err, ErrSessionCookieTooLarge) || size <= MaxCookieSize && err != nil {
			t.Fatalf("cookie size %d: %v", size, err)
		}
	}
	if _, err := parseSessionJWT(strings.Repeat("!", MaxCookieSize+1), nil); !errors.Is(err, ErrSessionCookieTooLarge) {
		t.Fatalf("JWT size not checked before parsing: %v", err)
	}
	for _, store := range []SessionStore{setupCookieStore(t), revocationStore(t)} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(&http.Cookie{Name: "session", Value: strings.Repeat("a", MaxCookieSize)})
		if s, err := store.Get(r, "session"); !errors.Is(err, ErrSessionCookieTooLarge) || s != nil {
			t.Fatalf("%T accepted oversized cookie: %v", store, err)
		}
		s := newSession(store, strings.Repeat("a", MaxCookieSize))
		w := httptest.NewRecorder()
		if err := s.Save(w, r); !errors.Is(err, ErrSessionCookieTooLarge) || len(w.Result().Cookies()) != 0 {
			t.Fatalf("%T wrote oversized cookie: %v", store, err)
		}
	}
}

func TestCookieStorePayloadBoundary(t *testing.T) {
	for _, scheme := range []string{"http", "https"} {
		t.Run(scheme, func(t *testing.T) {
			store := setupCookieStore(t)
			r := httptest.NewRequest(http.MethodGet, scheme+"://example.invalid/", nil)
			s := newSession(store, "boundary-session")
			// Find the largest payload through Save, including both base64 layers,
			// the JWT signature, and the cookie's name and attributes.
			low, high := 0, MaxCookieSize
			for low < high {
				size := (low + high + 1) / 2
				s.Data["payload"] = strings.Repeat("a", size)
				w := httptest.NewRecorder()
				err := store.Save(w, r, s)
				if errors.Is(err, ErrSessionCookieTooLarge) {
					if len(w.Header().Values("Set-Cookie")) != 0 {
						t.Fatal("rejected save issued a cookie")
					}
					high = size - 1
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				low = size
			}

			payload := strings.Repeat("a", low)
			s.Data["payload"] = payload
			w := httptest.NewRecorder()
			if err := store.Save(w, r, s); err != nil {
				t.Fatal(err)
			}
			// Base64 expands in blocks, so the largest accepted cookie can leave
			// up to five bytes unused before one more payload byte crosses the limit.
			size := len(w.Header().Get("Set-Cookie"))
			if size > MaxCookieSize || MaxCookieSize-size > 5 {
				t.Fatalf("boundary cookie size = %d, limit = %d", size, MaxCookieSize)
			}
			cookies := w.Result().Cookies()
			if len(cookies) != 1 || cookies[0].Secure != (scheme == "https") {
				t.Fatalf("unexpected boundary cookie: %v", cookies)
			}
			r.AddCookie(cookies[0])
			loaded, err := store.Get(r, s.Name)
			if err != nil || loaded.ID != s.ID || loaded.Data["payload"] != payload {
				t.Fatalf("boundary round trip failed: %v", err)
			}

			loaded.Data["payload"] = payload + "a"
			w = httptest.NewRecorder()
			if err := store.Save(w, r, loaded); !errors.Is(err, ErrSessionCookieTooLarge) || len(w.Header().Values("Set-Cookie")) != 0 {
				t.Fatalf("one-byte-larger payload was not rejected cleanly: %v", err)
			}
			loaded, err = store.Get(r, s.Name)
			if err != nil || loaded.Data["payload"] != payload {
				t.Fatalf("rejected save changed the original cookie session: %v", err)
			}
		})
	}
}

func TestOversizedSessionSavePreservesStorage(t *testing.T) {
	for _, store := range []SessionStore{setupCookieStore(t), revocationStore(t)} {
		s, r := savedSession(t, store)
		s.Data["large"] = strings.Repeat("a", MaxSessionDataSize)
		w := httptest.NewRecorder()
		if err := s.Save(w, r); !errors.Is(err, ErrSessionTooLarge) || len(w.Result().Cookies()) != 0 {
			t.Fatalf("%T accepted large session: %v", store, err)
		}
		loaded, err := store.Get(r, s.Name)
		if err != nil || loaded.Data["identity"] != "account" || len(loaded.Data) != 1 {
			t.Fatalf("%T changed persisted data: %+v, %v", store, loaded, err)
		}
	}
	cookieStore := setupCookieStore(t)
	s := newSession(cookieStore, "large")
	s.Data["large"] = strings.Repeat("a", MaxCookieSize)
	w := httptest.NewRecorder()
	if err := s.Save(w, httptest.NewRequest(http.MethodGet, "/", nil)); !errors.Is(err, ErrSessionCookieTooLarge) || len(w.Result().Cookies()) != 0 {
		t.Fatalf("cookie limit not enforced after JWT encoding: %v", err)
	}
}

func TestSQLiteSessionDataStaysServerSide(t *testing.T) {
	store := revocationStore(t)
	s := newSession(store, "private")
	s.Data["secret"] = strings.Repeat("private data", 1000)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if err := s.Save(w, r); err != nil {
		t.Fatal(err)
	}
	cookie := w.Result().Cookies()[0]
	claims, err := parseSessionJWT(cookie.Value, store.JwtKey)
	if err != nil || claims.Value != "" || claims.ID != s.ID {
		t.Fatalf("SQLite cookie leaked session data: %+v, %v", claims, err)
	}
	r.AddCookie(cookie)
	loaded, err := store.Get(r, s.Name)
	if err != nil || loaded.Data["secret"] != s.Data["secret"] {
		t.Fatalf("large server-side session failed: %v", err)
	}
	if _, err := store.db.Exec(`UPDATE sessions SET session = ? WHERE id = ?`, strings.Repeat("a", maxEncodedSessionSize+100), s.ID); err != nil {
		t.Fatal(err)
	}
	if loaded, err := store.Get(r, s.Name); !errors.Is(err, ErrSessionTooLarge) || loaded != nil {
		t.Fatalf("oversized stored data accepted: %v", err)
	}
}

func TestOversizedSessionRotationPreservesStorage(t *testing.T) {
	store := revocationStore(t)
	s, r := savedSession(t, store)
	id := s.ID
	w := httptest.NewRecorder()
	if err := s.Rotate(w, r, map[string]any{"large": strings.Repeat("a", MaxSessionDataSize)}); !errors.Is(err, ErrSessionTooLarge) {
		t.Fatalf("oversized rotation: %v", err)
	}
	loaded, err := store.Get(r, s.Name)
	if err != nil || loaded.ID != id || loaded.Data["identity"] != "account" || s.ID != id || len(w.Result().Cookies()) != 0 {
		t.Fatalf("failed rotation changed session: %+v, %v", loaded, err)
	}
}

func FuzzLoadSession(f *testing.F) {
	for _, raw := range []string{`{}`, `{"n":9223372036854775807}`, `null`, `{} {}`, "invalid"} {
		f.Add(base64.URLEncoding.EncodeToString([]byte(raw)))
	}
	f.Fuzz(func(t *testing.T, encoded string) {
		s, err := loadSession("id", "name", time.Time{}, nil, encoded)
		if err == nil && (s == nil || s.Data == nil) {
			t.Fatal("successful decode returned no session data")
		}
	})
}
