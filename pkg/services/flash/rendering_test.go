package flash

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/htmx"
	"github.com/trentnix/hyperserver/pkg/components/messages"
	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/session"
)

func TestRenderingAcknowledgesNotifications(t *testing.T) {
	for _, provider := range []string{"cookieStore", "sqliteStore"} {
		for _, hx := range []bool{false, true} {
			for _, outcome := range []string{"success", "HEAD", "template failure", "out-of-band failure", "empty body", "redirect", "refresh", "204"} {
				t.Run(fmt.Sprintf("%s/htmx=%t/%s", provider, hx, outcome), func(t *testing.T) {
					request := messageStoreRequests(t, provider)
					seed := httptest.NewRecorder()
					if err := AddSuccessNotification(seed, request(nil), "retained <notice>"); err != nil {
						t.Fatal(err)
					}
					cookies := seed.Result().Cookies()
					r := request(cookies)
					r.Header.Set("HX-Request", fmt.Sprint(hx))
					cm := contentservice.NewContentManager()
					cm.LoadNotifications = LoadNotifications
					page := content.NewManagedContent(r, cm)
					page.Data = []string{}
					source := `{{range .Notifications}}{{.Message}}{{end}}`
					switch outcome {
					case "HEAD":
						r.Method = http.MethodHead
					case "template failure":
						source += `{{index .Data 99}}`
					case "out-of-band failure":
						source += `{{define "broken"}}{{index .Data 99}}{{end}}`
						page.OutOfBand = []string{"broken"}
					case "empty body":
						source = `{{/* deliberately empty */}}`
					case "redirect":
						page.RedirectURL = "/next"
					case "refresh":
						page.HTMX.Response = &htmx.Response{Refresh: true}
					case "204":
						page.ResponseStatusCode = http.StatusNoContent
					}
					page.AddContent(fstest.MapFS{"page.html": {Data: []byte(source)}}, "page.html")
					w := httptest.NewRecorder()
					err := page.Render(w, r)
					failed := outcome == "template failure" || outcome == "out-of-band failure" && hx
					if (err != nil) != failed {
						t.Fatalf("render error = %v, want failure=%t", err, failed)
					}
					consumed := outcome == "success" || (outcome == "out-of-band failure" || outcome == "refresh") && !hx
					if consumed {
						if !strings.Contains(w.Body.String(), "retained &lt;notice&gt;") || len(w.Result().Cookies()) != 1 {
							t.Fatal("successful rendering did not display and acknowledge notifications")
						}
						if w.Result().Cookies()[0].MaxAge != -1 {
							t.Fatal("empty message session did not expire its cookie")
						}
						cookies = nil // Browsers discard expired cookies.
					} else if len(w.Header().Values("Set-Cookie")) != 0 || w.Body.Len() != 0 {
						t.Fatalf("undisplayed notifications changed response: %v %q", w.Header(), w.Body.String())
					}
					got, ack, err := LoadNotifications(request(cookies))
					if err != nil || (len(got) == 0) != consumed || !consumed && (len(got) != 1 || got[0].Message != "retained <notice>" || ack == nil) {
						t.Fatalf("remaining notifications = %v, %v", got, err)
					}
					if outcome == "refresh" && hx {
						if w.Header().Get("HX-Refresh") != "true" {
							t.Fatal("refresh instruction was lost")
						}
						page.HTMX.Response = nil
						next := httptest.NewRecorder()
						if err := page.Render(next, request(cookies)); err != nil || !strings.Contains(next.Body.String(), "retained &lt;notice&gt;") {
							t.Fatalf("reloaded page lost the notification: %q, %v", next.Body.String(), err)
						}
					}
				})
			}
		}
	}
}

func TestNotificationAcknowledgmentPreservesLaterMessages(t *testing.T) {
	for _, provider := range []string{"cookieStore", "sqliteStore"} {
		t.Run(provider, func(t *testing.T) {
			request := messageStoreRequests(t, provider)
			seed := httptest.NewRecorder()
			if err := AddSuccessNotification(seed, request(nil), "first"); err != nil {
				t.Fatal(err)
			}
			r := request(seed.Result().Cookies())
			got, ack, err := LoadNotifications(r)
			if err != nil || len(got) != 1 || ack == nil {
				t.Fatalf("load = %v, %v", got, err)
			}
			if err := AddSuccessNotification(httptest.NewRecorder(), r, "later"); err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			if err := ack(w); err != nil {
				t.Fatal(err)
			}
			repeated := httptest.NewRecorder()
			if err := ack(repeated); err != nil || len(repeated.Header().Values("Set-Cookie")) != 0 {
				t.Fatal("repeated acknowledgment was not a no-op")
			}
			got, _, err = LoadNotifications(request(w.Result().Cookies()))
			if err != nil || len(got) != 1 || got[0].Message != "later" {
				t.Fatalf("acknowledgment removed later messages: %v, %v", got, err)
			}
		})
	}
}

func TestLoadErrorNotificationsPreservesUnselectedMessages(t *testing.T) {
	errorNotice := messages.NewNotification("same text", messages.NotificationTypeError)
	successNotice := messages.NewNotification("same text", messages.NotificationTypeSuccess)
	for _, provider := range []string{"cookieStore", "sqliteStore"} {
		for _, tc := range []struct {
			name     string
			initial  []messages.Notification
			selected int
			later    []messages.Notification
			retained []messages.Notification
		}{
			{name: "empty"},
			{name: "only successes", initial: []messages.Notification{successNotice}, retained: []messages.Notification{successNotice}},
			{name: "only errors", initial: []messages.Notification{errorNotice}, selected: 1},
			{
				name:    "mixed with later additions",
				initial: []messages.Notification{successNotice, errorNotice, successNotice, errorNotice}, selected: 2,
				later: []messages.Notification{errorNotice, successNotice}, retained: []messages.Notification{successNotice, successNotice, errorNotice, successNotice},
			},
		} {
			t.Run(provider+"/"+tc.name, func(t *testing.T) {
				request := messageStoreRequests(t, provider)
				r := request(nil)
				seed := httptest.NewRecorder()
				for _, notice := range tc.initial {
					seed = httptest.NewRecorder()
					if err := addNotification(seed, r, notice); err != nil {
						t.Fatal(err)
					}
				}
				r = request(seed.Result().Cookies())
				got, ack, err := LoadErrorNotifications(r)
				if err != nil || len(got) != tc.selected || (ack == nil) != (tc.selected == 0) {
					t.Fatalf("selected errors = %v, %v", got, err)
				}
				for _, notice := range got {
					if notice != errorNotice {
						t.Fatalf("selected a non-error: %v", notice)
					}
				}
				if all, _, err := LoadNotifications(request(seed.Result().Cookies())); err != nil || !reflect.DeepEqual(all, tc.initial) {
					t.Fatalf("loading changed storage: %v, %v", all, err)
				}
				if ack == nil {
					return
				}

				for _, notice := range tc.later {
					seed = httptest.NewRecorder()
					if err := addNotification(seed, r, notice); err != nil {
						t.Fatal(err)
					}
				}
				s, err := session.Get(r, messagesSession.String())
				if err != nil {
					t.Fatal(err)
				}
				failure := errors.New("removal unavailable")
				store := &failingMessageStore{SessionStore: s.Store, err: failure}
				s.Store = store
				w := httptest.NewRecorder()
				if err := ack(w); !errors.Is(err, failure) || len(w.Header()) != 0 {
					t.Fatalf("failed acknowledgment = %v, headers=%v", err, w.Header())
				}
				wantAll := append(append([]messages.Notification(nil), tc.initial...), tc.later...)
				for _, check := range []*http.Request{r, request(seed.Result().Cookies())} {
					if all, _, err := LoadNotifications(check); err != nil || !reflect.DeepEqual(all, wantAll) {
						t.Fatalf("failed acknowledgment changed messages: %v, %v", all, err)
					}
				}
				store.err = nil
				if err := ack(w); err != nil {
					t.Fatalf("acknowledgment retry: %v", err)
				}
				cookies := w.Result().Cookies()
				if len(cookies) != 1 {
					t.Fatal("successful acknowledgment did not update the cookie")
				}
				if cookies[0].MaxAge == -1 {
					cookies = nil
				}
				if all, _, err := LoadNotifications(request(cookies)); err != nil || !reflect.DeepEqual(all, tc.retained) {
					t.Fatalf("retained messages = %v, %v, want %v", all, err, tc.retained)
				}
			})
		}
	}
}

func TestHeadDoesNotRetrieveMessageCategories(t *testing.T) {
	for _, provider := range []string{"cookieStore", "sqliteStore"} {
		for _, category := range []string{categoryNotification, flashMessageCategory(AuthMessages), categorySystemMessage} {
			t.Run(provider+"/"+category, func(t *testing.T) {
				request := messageStoreRequests(t, provider)
				seed := httptest.NewRecorder()
				if err := addMessage(seed, request(nil), "retained", "test", category); err != nil {
					t.Fatal(err)
				}
				r := request(seed.Result().Cookies())
				r.Method = http.MethodHead
				w := httptest.NewRecorder()
				if got, err := getMessages(w, r, category); err != nil || len(got) != 0 || len(w.Header()) != 0 {
					t.Fatalf("HEAD retrieved messages: %v, %v", got, err)
				}
				got, _, err := loadMessages(request(seed.Result().Cookies()), category)
				if err != nil || len(got) != 1 || got[0].Message != "retained" {
					t.Fatalf("HEAD discarded messages: %v, %v", got, err)
				}
			})
		}
	}
}

func TestNotificationAcknowledgmentRejectsChangedCategory(t *testing.T) {
	for _, provider := range []string{"cookieStore", "sqliteStore"} {
		for _, changed := range []any{"malformed", []map[string]string{{"message": "replacement", "messageType": "test"}}} {
			t.Run(fmt.Sprintf("%s/%T", provider, changed), func(t *testing.T) {
				request := messageStoreRequests(t, provider)
				r := request(nil)
				if err := AddSuccessNotification(httptest.NewRecorder(), r, "original"); err != nil {
					t.Fatal(err)
				}
				_, ack, err := LoadNotifications(r)
				if err != nil || ack == nil {
					t.Fatalf("load failed: %v", err)
				}
				s, err := session.Get(r, messagesSession.String())
				if err != nil {
					t.Fatal(err)
				}
				original := s.Data[categoryNotification]
				s.Data[categoryNotification] = changed
				w := httptest.NewRecorder()
				var removal *ErrDeletingContentMessages
				if err := ack(w); !errors.As(err, &removal) || len(w.Header()) != 0 {
					t.Fatalf("changed category acknowledged: %v", err)
				}
				s.Data[categoryNotification] = original
				if err := ack(w); err != nil {
					t.Fatalf("acknowledgment retry failed: %v", err)
				}
			})
		}
	}
}

func TestRenderingReportsNotificationAcknowledgmentFailure(t *testing.T) {
	for _, provider := range []string{"cookieStore", "sqliteStore"} {
		for _, operation := range []string{"save", "end"} {
			t.Run(provider+"/"+operation, func(t *testing.T) {
				request := messageStoreRequests(t, provider)
				seed := httptest.NewRecorder()
				r := request(nil)
				if err := AddSuccessNotification(seed, r, "retained"); err != nil {
					t.Fatal(err)
				}
				if operation == "save" {
					seed = httptest.NewRecorder()
					if err := AddMessage(seed, r, "other category", AuthMessages); err != nil {
						t.Fatal(err)
					}
				}
				r = request(seed.Result().Cookies())
				s, err := session.Get(r, messagesSession.String())
				if err != nil {
					t.Fatal(err)
				}
				failure := errors.New("acknowledgment storage unavailable")
				store := &failingMessageStore{SessionStore: s.Store, err: failure}
				s.Store = store
				cm := contentservice.NewContentManager()
				cm.LoadNotifications = LoadNotifications
				page := content.NewManagedContent(r, cm)
				page.AddContent(fstest.MapFS{"page.html": {Data: []byte(`{{range .Notifications}}{{.Message}}{{end}}`)}}, "page.html")
				w := httptest.NewRecorder()
				if err := page.Render(w, r); !errors.Is(err, failure) {
					t.Fatalf("acknowledgment failure = %v", err)
				}
				if w.Body.Len() != 0 || len(w.Header()) != 0 {
					t.Fatal("failed acknowledgment committed a response")
				}
				got, _, err := LoadNotifications(request(seed.Result().Cookies()))
				if err != nil || len(got) != 1 || got[0].Message != "retained" {
					t.Fatalf("failed acknowledgment removed notifications: %v, %v", got, err)
				}
				store.err = nil
				if err := page.Render(w, r); err != nil || w.Body.String() != "retained" {
					t.Fatalf("render retry = %q, %v", w.Body.String(), err)
				}
				cookies := w.Result().Cookies()
				if operation == "end" {
					if len(cookies) != 1 || cookies[0].MaxAge != -1 {
						t.Fatal("successful retry did not expire the cookie")
					}
					cookies = nil
				}
				if got, _, err := LoadNotifications(request(cookies)); err != nil || len(got) != 0 {
					t.Fatalf("retry did not remove notifications: %v, %v", got, err)
				}
			})
		}
	}
}
