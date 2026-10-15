package cookie

import (
	"net/http/httptest"
	"testing"
	"testing/synctest"
	"time"

	"github.com/trentnix/hyperserver/internal/test/sessiontest"
	"github.com/trentnix/hyperserver/pkg/services/session"
)

func TestCookieSessionContract(t *testing.T) {
	sessiontest.RunCommon(t, func(t *testing.T) session.SessionStore {
		return setupCookieStore(t)
	})
}

func TestRepeatedSavePreservesCookieExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := setupCookieStore(t)
		r := httptest.NewRequest("GET", "/", nil)
		s := session.NewSession(store, "account")
		if err := s.Save(httptest.NewRecorder(), r); err != nil {
			t.Fatal(err)
		}
		expiry := s.ExpiresAt
		time.Sleep(2 * time.Second)
		w := httptest.NewRecorder()
		if err := s.Save(w, r); err != nil {
			t.Fatal(err)
		}
		if s.IsNew || !s.ExpiresAt.Equal(expiry) {
			t.Fatal("second save renewed the session")
		}
		cookies := w.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatalf("cookies = %d, want one", len(cookies))
		}
		r.AddCookie(cookies[0])
		loaded, err := store.Get(r, s.Name)
		if err != nil || loaded.IsNew || !loaded.ExpiresAt.Equal(expiry) {
			t.Fatalf("cookie lost original expiry: %+v, %v", loaded, err)
		}

		time.Sleep(time.Until(expiry))
		expired, err := store.Get(r, s.Name)
		if err != nil || !expired.IsNew || expired.ID == s.ID {
			t.Fatalf("repeated save extended cookie validity: %+v, %v", expired, err)
		}
	})
}
