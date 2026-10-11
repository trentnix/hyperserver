package stores

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sessions "github.com/trentnix/hyperserver/pkg/services/session"
)

func TestSessionCookieSizeLimits(t *testing.T) {
	cookie := &http.Cookie{Name: "session", Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode}
	for _, size := range []int{sessions.MaxCookieSize - 1, sessions.MaxCookieSize, sessions.MaxCookieSize + 1} {
		cookie.Value = ""
		cookie.Value = strings.Repeat("a", size-len(cookie.String()))
		err := validateCookie(cookie)
		if size > sessions.MaxCookieSize && !errors.Is(err, sessions.ErrSessionCookieTooLarge) || size <= sessions.MaxCookieSize && err != nil {
			t.Fatalf("cookie size %d: %v", size, err)
		}
	}
	if _, err := parseSessionJWT(strings.Repeat("!", sessions.MaxCookieSize+1), nil); !errors.Is(err, sessions.ErrSessionCookieTooLarge) {
		t.Fatalf("JWT size not checked before parsing: %v", err)
	}
	for _, store := range []sessions.SessionStore{setupCookieStore(t), revocationStore(t)} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(&http.Cookie{Name: "session", Value: strings.Repeat("a", sessions.MaxCookieSize)})
		if s, err := store.Get(r, "session"); !errors.Is(err, sessions.ErrSessionCookieTooLarge) || s != nil {
			t.Fatalf("%T accepted oversized cookie: %v", store, err)
		}
		s := sessions.NewSession(store, strings.Repeat("a", sessions.MaxCookieSize))
		w := httptest.NewRecorder()
		if err := s.Save(w, r); !errors.Is(err, sessions.ErrSessionCookieTooLarge) || len(w.Result().Cookies()) != 0 {
			t.Fatalf("%T wrote oversized cookie: %v", store, err)
		}
	}
}

func TestCookieStorePayloadBoundary(t *testing.T) {
	for _, scheme := range []string{"http", "https"} {
		t.Run(scheme, func(t *testing.T) {
			store := setupCookieStore(t)
			r := httptest.NewRequest(http.MethodGet, scheme+"://example.invalid/", nil)
			s := sessions.NewSession(store, "boundary-session")
			// Find the largest payload through Save, including both base64 layers,
			// the JWT signature, and the cookie's name and attributes.
			low, high := 0, sessions.MaxCookieSize
			for low < high {
				size := (low + high + 1) / 2
				s.Data["payload"] = strings.Repeat("a", size)
				w := httptest.NewRecorder()
				err := store.Save(w, r, s)
				if errors.Is(err, sessions.ErrSessionCookieTooLarge) {
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
			if size > sessions.MaxCookieSize || sessions.MaxCookieSize-size > 5 {
				t.Fatalf("boundary cookie size = %d, limit = %d", size, sessions.MaxCookieSize)
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
			if err := store.Save(w, r, loaded); !errors.Is(err, sessions.ErrSessionCookieTooLarge) || len(w.Header().Values("Set-Cookie")) != 0 {
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
	for _, store := range []sessions.SessionStore{setupCookieStore(t), revocationStore(t)} {
		s, r := savedSession(t, store)
		s.Data["large"] = strings.Repeat("a", sessions.MaxSessionDataSize)
		w := httptest.NewRecorder()
		if err := s.Save(w, r); !errors.Is(err, sessions.ErrSessionTooLarge) || len(w.Result().Cookies()) != 0 {
			t.Fatalf("%T accepted large session: %v", store, err)
		}
		loaded, err := store.Get(r, s.Name)
		if err != nil || loaded.Data["identity"] != "account" || len(loaded.Data) != 1 {
			t.Fatalf("%T changed persisted data: %+v, %v", store, loaded, err)
		}
	}
	cookieStore := setupCookieStore(t)
	s := sessions.NewSession(cookieStore, "large")
	s.Data["large"] = strings.Repeat("a", sessions.MaxCookieSize)
	w := httptest.NewRecorder()
	if err := s.Save(w, httptest.NewRequest(http.MethodGet, "/", nil)); !errors.Is(err, sessions.ErrSessionCookieTooLarge) || len(w.Result().Cookies()) != 0 {
		t.Fatalf("cookie limit not enforced after JWT encoding: %v", err)
	}
}

func TestSQLiteSessionDataStaysServerSide(t *testing.T) {
	store := revocationStore(t)
	s := sessions.NewSession(store, "private")
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
	if _, err := store.db.Exec(`UPDATE sessions SET session = ? WHERE id = ?`, strings.Repeat("a", sessions.MaxEncodedSessionSize+100), s.ID); err != nil {
		t.Fatal(err)
	}
	if loaded, err := store.Get(r, s.Name); !errors.Is(err, sessions.ErrSessionTooLarge) || loaded != nil {
		t.Fatalf("oversized stored data accepted: %v", err)
	}
}

func TestOversizedSessionRotationPreservesStorage(t *testing.T) {
	store := revocationStore(t)
	s, r := savedSession(t, store)
	id := s.ID
	w := httptest.NewRecorder()
	if err := s.Rotate(w, r, map[string]any{"large": strings.Repeat("a", sessions.MaxSessionDataSize)}); !errors.Is(err, sessions.ErrSessionTooLarge) {
		t.Fatalf("oversized rotation: %v", err)
	}
	loaded, err := store.Get(r, s.Name)
	if err != nil || loaded.ID != id || loaded.Data["identity"] != "account" || s.ID != id || len(w.Result().Cookies()) != 0 {
		t.Fatalf("failed rotation changed session: %+v, %v", loaded, err)
	}
}
