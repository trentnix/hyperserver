package flash

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/trentnix/hyperserver/pkg/components/content"
	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/session"
)

func TestRenderingDiscardsInvalidMessageCookies(t *testing.T) {
	legacy, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"id": "old-session", "name": messagesSession.String(), "purpose": "session",
		"exp": time.Now().Add(time.Hour).Unix(),
	}).SignedString([]byte("old-signing-key"))
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range []string{"sqliteStore", "cookieStore"} {
		for _, raw := range []string{legacy, "malformed"} {
			for _, hx := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/htmx=%t", provider, raw, hx), func(t *testing.T) {
					request := messageStoreRequests(t, provider)
					cookies := []*http.Cookie{{Name: messagesSession.String(), Value: raw}}
					for attempt := range 2 {
						r := request(cookies)
						r.Header.Set("HX-Request", fmt.Sprint(hx))
						cm := contentservice.NewContentManager()
						cm.LoadNotifications = LoadNotifications
						page := content.NewManagedContent(r, cm)
						prepareContent(t, page, content.TemplatePath{FS: fstest.MapFS{"page.html": {Data: []byte(`ready{{range .Notifications}}{{.Message}}{{end}}`)}}, Path: "page.html"})
						w := httptest.NewRecorder()
						if err := page.Render(w, r); err != nil || w.Code != http.StatusOK || w.Body.String() != "ready" {
							t.Fatalf("render failed: status=%d, body=%q, error=%v", w.Code, w.Body.String(), err)
						}
						got := w.Result().Cookies()
						if attempt == 0 {
							if len(got) != 1 || got[0].Name != messagesSession.String() || got[0].MaxAge != -1 {
								t.Fatalf("invalid cookie not expired: %v", got)
							}
							cookies = nil // Apply the deletion as a browser would.
						} else if len(got) != 0 {
							t.Fatal("clean request unexpectedly changed cookies")
						}
					}
				})
			}
		}
	}
}

func TestInvalidMessageCookieReplacementSurvivesAcknowledgment(t *testing.T) {
	for _, provider := range []string{"sqliteStore", "cookieStore"} {
		t.Run(provider, func(t *testing.T) {
			request := messageStoreRequests(t, provider)
			r := request([]*http.Cookie{{Name: messagesSession.String(), Value: "malformed"}})
			got, ack, err := LoadNotifications(r)
			if err != nil || len(got) != 0 || ack == nil {
				t.Fatalf("invalid cookie load: %v, %v", got, err)
			}
			w := httptest.NewRecorder()
			if err := AddSuccessNotification(w, r, "new notice"); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if err := ack(w); err != nil {
					t.Fatal(err)
				}
			}
			cookies := w.Result().Cookies()
			if len(cookies) != 1 || cookies[0].MaxAge <= 0 {
				t.Fatalf("replacement cookie discarded: %v", cookies)
			}
			got, _, err = LoadNotifications(request(cookies))
			if err != nil || len(got) != 1 || got[0].Message != "new notice" {
				t.Fatalf("replacement message lost: %v, %v", got, err)
			}
		})
	}
}

func TestInvalidMessageCookieWaitsForSuccessfulRender(t *testing.T) {
	request := messageStoreRequests(t, "sqliteStore")
	r := request([]*http.Cookie{{Name: messagesSession.String(), Value: "malformed"}})
	cm := contentservice.NewContentManager()
	cm.LoadNotifications = LoadNotifications
	page := content.NewManagedContent(r, cm)
	page.Data = []string{}
	prepareContent(t, page, content.TemplatePath{FS: fstest.MapFS{"page.html": {Data: []byte(`{{index .Data 0}}`)}}, Path: "page.html"})
	w := httptest.NewRecorder()
	if err := page.Render(w, r); err == nil || len(w.Header()) != 0 || w.Body.Len() != 0 {
		t.Fatalf("failed rendering changed response: %v, %v", err, w.Header())
	}
	page.Data = []string{"ready"}
	if err := page.Render(w, r); err != nil || w.Body.String() != "ready" {
		t.Fatalf("render retry failed: %v", err)
	}
	if cookies := w.Result().Cookies(); len(cookies) != 1 || cookies[0].MaxAge != -1 {
		t.Fatalf("render retry did not discard invalid cookie: %v", cookies)
	}
}

type messageLoadFailureStore struct {
	session.SessionStore
	err error
}

func (s *messageLoadFailureStore) Get(*http.Request, string) (*session.Session, error) {
	return nil, s.err
}

func TestMessageCookieRecoveryPreservesStorageFailures(t *testing.T) {
	failure := errors.New("storage unavailable")
	store := &messageLoadFailureStore{err: failure}
	manager, err := session.NewSessionManager(t.Context(), map[string]string{"default": "test"}, map[string]session.StoreFactory{
		"test": func(context.Context) (session.SessionStore, error) { return store, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.Close() })
	r := session.AddSessionManagerToRequestContext(httptest.NewRequest("GET", "/", nil), manager)
	w := httptest.NewRecorder()
	if _, _, err := LoadNotifications(r); !errors.Is(err, failure) {
		t.Fatalf("load hid storage failure: %v", err)
	}
	if err := AddSuccessNotification(w, r, "new"); !errors.Is(err, failure) || len(w.Header()) != 0 {
		t.Fatalf("add hid storage failure: %v", err)
	}
	store.err = session.ErrInvalidToken
	_, ack, err := LoadNotifications(r)
	if err != nil || ack == nil {
		t.Fatal("invalid cookie did not provide acknowledgment", err)
	}
	store.err = failure
	if err := ack(w); !errors.Is(err, failure) || len(w.Header()) != 0 {
		t.Fatalf("acknowledgment hid storage failure: %v", err)
	}
}
