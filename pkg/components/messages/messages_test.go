package messages

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/session"
)

func messageRequests(t *testing.T) func([]*http.Cookie) *http.Request {
	return messageStoreRequests(t, "cookieStore")
}

func messageStoreRequests(t *testing.T, provider string) func([]*http.Cookie) *http.Request {
	t.Helper()
	cfg := &config.Config{}
	cfg.HTTP.Session.JwtKey = "message-test-key"
	cfg.HTTP.Session.TokenAge = time.Hour
	cfg.HTTP.Session.CookieAge = time.Hour
	cfg.HTTP.Session.Types = map[string]string{"default": provider}
	cfg.HTTP.Session.Stores = map[string]map[string]string{provider: {
		"enabled": "true", "connection": filepath.Join(t.TempDir(), "messages.db"), "sessiontable": "messages",
	}}
	manager, err := session.NewSessionManager(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.Close() })
	return func(cookies []*http.Cookie) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		for _, cookie := range cookies {
			r.AddCookie(cookie)
		}
		return session.AddSessionManagerToRequestContext(r, manager)
	}
}

func TestMessageCategoriesAreConsumedAcrossRequests(t *testing.T) {
	for _, provider := range []string{"cookieStore", "sqliteStore"} {
		t.Run(provider, func(t *testing.T) {
			request := messageStoreRequests(t, provider)
			jar, err := cookiejar.New(nil)
			if err != nil {
				t.Fatal(err)
			}
			origin := &url.URL{Scheme: "http", Host: "example.com", Path: "/"}
			roundTrip := func(action func(http.ResponseWriter, *http.Request)) *httptest.ResponseRecorder {
				w := httptest.NewRecorder()
				action(w, request(jar.Cookies(origin)))
				jar.SetCookies(origin, w.Result().Cookies())
				return w
			}
			for _, add := range []func(http.ResponseWriter, *http.Request) error{
				func(w http.ResponseWriter, r *http.Request) error { return AddMessage(w, r, "auth", AuthMessages) },
				func(w http.ResponseWriter, r *http.Request) error {
					return AddSuccessNotification(w, r, "notification")
				},
				func(w http.ResponseWriter, r *http.Request) error { return AddSystemInfoMessage(w, r, "console") },
			} {
				roundTrip(func(w http.ResponseWriter, r *http.Request) {
					if err := add(w, r); err != nil {
						t.Fatal(err)
					}
				})
			}
			getters := []struct {
				want string
				get  func(http.ResponseWriter, *http.Request) ([]string, error)
			}{
				{"auth", func(w http.ResponseWriter, r *http.Request) ([]string, error) {
					msgs, err := GetMessages(w, r, AuthMessages)
					var text []string
					for _, msg := range msgs {
						text = append(text, msg.Message)
					}
					return text, err
				}},
				{"notification", func(w http.ResponseWriter, r *http.Request) ([]string, error) {
					msgs, err := GetNotifications(w, r)
					var text []string
					for _, msg := range msgs {
						text = append(text, msg.Message)
					}
					return text, err
				}},
				{"console", func(w http.ResponseWriter, r *http.Request) ([]string, error) {
					msgs, err := GetSystemMessages(w, r)
					var text []string
					for _, msg := range msgs {
						text = append(text, msg.Message)
					}
					return text, err
				}},
			}
			for i, getter := range getters {
				w := roundTrip(func(w http.ResponseWriter, r *http.Request) {
					got, err := getter.get(w, r)
					if err != nil || len(got) != 1 || got[0] != getter.want {
						t.Fatalf("%s retrieval = %v, %v", getter.want, got, err)
					}
				})
				cookies := w.Result().Cookies()
				if len(cookies) != 1 || (cookies[0].MaxAge == -1) != (i == len(getters)-1) {
					t.Fatalf("wrong save/end cookie: %v", cookies)
				}
				// A fresh request must not see any already-consumed category again.
				for _, consumed := range getters[:i+1] {
					w := roundTrip(func(w http.ResponseWriter, r *http.Request) {
						got, err := consumed.get(w, r)
						if err != nil || len(got) != 0 {
							t.Fatalf("category replayed: %v, %v", got, err)
						}
					})
					if len(w.Result().Cookies()) != 0 {
						t.Fatal("missing category wrote a session cookie")
					}
				}
			}
		})
	}
}

type failingMessageStore struct {
	session.SessionStore
	err         error
	saves, ends int
}

func (s *failingMessageStore) Save(w http.ResponseWriter, r *http.Request, value *session.Session) error {
	s.saves++
	if s.err != nil {
		return s.err
	}
	return s.SessionStore.Save(w, r, value)
}

func (s *failingMessageStore) End(w http.ResponseWriter, r *http.Request, value *session.Session) error {
	s.ends++
	if s.err != nil {
		return s.err
	}
	return s.SessionStore.End(w, r, value)
}

func TestMessageRemovalFailurePreservesCategoryForRetry(t *testing.T) {
	for _, provider := range []string{"cookieStore", "sqliteStore"} {
		for _, operation := range []string{"save", "end"} {
			t.Run(provider+"/"+operation, func(t *testing.T) {
				request := messageStoreRequests(t, provider)
				r, w := request(nil), httptest.NewRecorder()
				if err := AddSuccessNotification(w, r, "retained"); err != nil {
					t.Fatal(err)
				}
				if operation == "save" {
					r, w = request(w.Result().Cookies()), httptest.NewRecorder()
					if err := AddMessage(w, r, "other", AuthMessages); err != nil {
						t.Fatal(err)
					}
				}
				cookies := w.Result().Cookies()
				r, w = request(cookies), httptest.NewRecorder()
				value, err := session.Get(r, messagesSession.String())
				if err != nil {
					t.Fatal(err)
				}
				before := maps.Clone(value.Data)
				failure := errors.New("storage failure")
				store := &failingMessageStore{SessionStore: value.Store, err: failure}
				value.Store = store
				got, err := GetNotifications(w, r)
				var removal *ErrDeletingContentMessages
				if got != nil || !errors.Is(err, failure) || !errors.As(err, &removal) || !reflect.DeepEqual(value.Data, before) || len(w.Result().Cookies()) != 0 {
					t.Fatalf("failed removal changed state: messages=%v, error=%v, data=%v", got, err, value.Data)
				}
				if (store.saves == 1) != (operation == "save") || (store.ends == 1) != (operation == "end") {
					t.Fatal("wrong persistence operation")
				}
				fresh, err := session.Get(request(cookies), messagesSession.String())
				if err != nil || !reflect.DeepEqual(fresh.Data, before) {
					t.Fatalf("failed removal changed stored state: %v", err)
				}
				store.err = nil
				got, err = GetNotifications(w, r)
				if err != nil || len(got) != 1 || got[0].Message != "retained" {
					t.Fatalf("retry failed: %v, %v", got, err)
				}
				if got, err := GetNotifications(httptest.NewRecorder(), request(w.Result().Cookies())); err != nil || len(got) != 0 {
					t.Fatalf("retry did not persist removal: %v, %v", got, err)
				}
			})
		}
	}
}

func TestMessagesJSONRoundTrip(t *testing.T) {
	request := messageRequests(t)
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
	request := messageRequests(t)
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
