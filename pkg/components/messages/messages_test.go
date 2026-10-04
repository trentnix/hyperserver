package messages

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/session"
)

func messageRequests() func([]*http.Cookie) *http.Request {
	cfg := &config.Config{}
	cfg.HTTP.Session.JwtKey = "message-test-key"
	cfg.HTTP.Session.TokenAge = time.Hour
	cfg.HTTP.Session.CookieAge = time.Hour
	cfg.HTTP.Session.Types = map[string]string{"default": "cookieStore"}
	cfg.HTTP.Session.Stores = map[string]map[string]string{"cookieStore": {"enabled": "true"}}
	manager := session.NewSessionManager(cfg)
	return func(cookies []*http.Cookie) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		for _, cookie := range cookies {
			r.AddCookie(cookie)
		}
		return session.AddSessionManagerToRequestContext(r, manager)
	}
}

func TestMessagesJSONRoundTrip(t *testing.T) {
	request := messageRequests()
	w := httptest.NewRecorder()
	r := request(nil)
	if err := AddSuccessNotification(w, r, "first"); err != nil {
		t.Fatal(err)
	}
	// Append after deserialization, then read both messages in another request.
	r = request(w.Result().Cookies())
	w = httptest.NewRecorder()
	if err := AddErrorNotification(w, r, "second"); err != nil {
		t.Fatal(err)
	}
	r = request(w.Result().Cookies())
	w = httptest.NewRecorder()
	messages, err := GetNotifications(w, r)
	if err != nil || len(messages) != 2 || messages[0].Message != "first" || !messages[0].IsSuccess() || messages[1].Message != "second" || !messages[1].IsError() {
		t.Fatalf("notifications = %+v, %v", messages, err)
	}
	if cookies := w.Result().Cookies(); len(cookies) != 1 || cookies[0].MaxAge != -1 {
		t.Fatalf("consumed message cookie not expired: %v", cookies)
	}
	if messages, err := GetNotifications(w, r); err != nil || len(messages) != 0 {
		t.Fatalf("consumed messages returned again: %+v, %v", messages, err)
	}
}

func TestMessagesRejectInvalidStoredValues(t *testing.T) {
	request := messageRequests()
	for _, value := range []any{"not a message array", []any{map[string]any{"message": 42}}} {
		r := request(nil)
		s, err := session.Get(r, messagesSession.String())
		if err != nil {
			t.Fatal(err)
		}
		s.Data[categoryNotification] = value
		w := httptest.NewRecorder()
		if err := AddSuccessNotification(w, r, "new"); err == nil {
			t.Fatal("malformed messages overwritten")
		}
		if _, err := GetNotifications(w, r); err == nil {
			t.Fatal("malformed messages accepted")
		}
		if len(s.Data) != 1 || len(w.Result().Cookies()) != 0 {
			t.Fatal("failed retrieval changed session")
		}
	}
}
