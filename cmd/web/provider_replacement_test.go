package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
	"github.com/trentnix/hyperserver/auth/password"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/internal/test/sessiontest"
	site "github.com/trentnix/hyperserver/modules/site"
	"github.com/trentnix/hyperserver/modules/site/models"
	framework "github.com/trentnix/hyperserver/pkg/application"
	hs_errors "github.com/trentnix/hyperserver/pkg/errors"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

// These fixtures exercise provider wiring, not a new production storage adapter.
// Unexpected account writes fail through the unimplemented embedded contract.
type replacementAccountReads struct {
	user.AccountRepository
	account user.User
	fail    error
	reads   int
}

func (p *replacementAccountReads) GetByID(ctx context.Context, id string) (*user.User, error) {
	p.reads++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.fail != nil {
		return nil, p.fail
	}
	if id != p.account.ID {
		return nil, user.ErrUserNotFound
	}
	copy := p.account
	return &copy, nil
}

func (p *replacementAccountReads) GetByEmail(ctx context.Context, email string) (*user.User, error) {
	if user.EmailKey(email) != user.EmailKey(p.account.Email) {
		return nil, user.ErrUserNotFound
	}
	return p.GetByID(ctx, p.account.ID)
}

type replacementContacts struct {
	submissions []models.ContactSubmission
	fail        error
}

func (p *replacementContacts) Create(ctx context.Context, submission *models.ContactSubmission) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.fail != nil {
		return p.fail
	}
	submission.Id, submission.CreatedAt = "contact-id", time.Now()
	p.submissions = append(p.submissions, *submission)
	return nil
}

func newReplacementHTTPHarness(t *testing.T) (*httpHarness, *replacementAccountReads, *replacementSessionStore, *replacementContacts) {
	t.Helper()
	cfg := config.Config{
		App:      config.AppConfig{Name: "Replacement providers", RenderNotifications: true},
		HTTP:     config.HTTPConfig{ListenHost: "127.0.0.1", Port: 8080},
		Database: config.DatabaseConfig{Driver: "must-not-open"},
		Mail:     config.MailConfig{Timeout: -time.Second},
		Auth: config.AuthConfig{
			Enabled: true, JwtKey: "replacement-account-test-key0000", ResetTokenExpiration: time.Hour,
			VerificationTokenExpiration: time.Hour,
			AccountStorage:              config.AccountStorageConfig{Provider: "must-not-open"},
			Services:                    map[string]map[string]string{"email": {"enabled": "true"}},
		},
	}
	cfg.HTTP.Session.Types = map[string]string{"default": "must-not-open"}
	hash, err := password.HashPassword("Password1!")
	if err != nil {
		t.Fatal(err)
	}
	accounts := &replacementAccountReads{account: user.User{
		ID: "account-id", Email: "person@example.invalid", Password: hash,
		SessionVersion: 1, RegistrationAuthType: "email", Verified: true,
	}}
	store := &replacementSessionStore{records: make(map[string]replacementSessionRecord)}
	manager, err := session.NewSessionManager(t.Context(), map[string]string{"default": "replacement"}, map[string]session.StoreFactory{
		"replacement": func(context.Context) (session.SessionStore, error) { return store, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.Close() })
	mail := &fakeMailSender{}
	mailClient, err := messaging.NewMailClientWithSender("sender@example.invalid", mail)
	if err != nil {
		t.Fatal(err)
	}

	contacts := &replacementContacts{}
	logs := &capturedLogs{}
	app, err := newApplication(logger.Set(t.Context(), newTestLogger(logs)), cfg, "", framework.Options{
		Services: framework.Services{Accounts: accounts, Sessions: manager, Mail: mailClient},
		Modules: func(context.Context, *framework.Application) ([]handlers.Descriptor, error) {
			return handlers.Replace(handlers.Registered(), site.Module(func(context.Context) (site.ContactRepository, error) { return contacts, nil }, mailClient))
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Shutdown(); err != nil {
			t.Error(err)
		}
	})
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &httpHarness{app: app, handler: app.Handler, baseURL: "http://127.0.0.1:8080", cookies: jar, mail: mail, logs: logs}, accounts, store, contacts
}

func TestHTTPApplicationSuppliedProviders(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)
	for _, htmx := range []bool{false, true} {
		h, accounts, sessions, contacts := newReplacementHTTPHarness(t)
		w := h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {accounts.account.Email}, "password": {"WrongPassword1!"}}, htmx)
		if w.Header().Get("HX-Redirect") != "" || w.Header().Get("Location") != "" || sessions.rotations != 0 {
			t.Fatal("replacement login accepted incorrect credentials")
		}
		w = h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {accounts.account.Email}, "password": {"Password1!"}}, htmx)
		if w.Header().Get("HX-Redirect") != "/" && w.Header().Get("Location") != "/" {
			t.Fatalf("replacement login failed: %d %s", w.Code, w.Body.String())
		}
		if sessions.rotations != 1 || accounts.reads == 0 {
			t.Fatal("login bypassed replacement accounts or session rotation")
		}
		w = h.request(http.MethodGet, "/", nil, htmx)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), accounts.account.Email) {
			t.Fatalf("replacement identity not loaded: %d %s", w.Code, w.Body.String())
		}
		w = h.request(http.MethodPost, "/test-email?to=recipient@example.invalid", nil, htmx)
		if w.Code != http.StatusOK || len(h.mail.snapshot()) != 1 {
			t.Fatalf("replacement mail failed: %d %s", w.Code, w.Body.String())
		}
		h.mail.err = errors.New("private delivery failure")
		w = h.request(http.MethodPost, "/test-email?to=recipient@example.invalid", nil, htmx)
		if w.Code != http.StatusInternalServerError || len(h.mail.snapshot()) != 2 || strings.Contains(w.Body.String(), "private delivery failure") {
			t.Fatal("replacement mail error was hidden or exposed")
		}
		h.mail.err = nil
		form := url.Values{"name": {"Person"}, "email": {accounts.account.Email}, "message": {"Replacement contact storage"}}
		w = h.request(http.MethodPost, "/contact", form, htmx)
		if w.Code != http.StatusOK || len(contacts.submissions) != 1 || !strings.Contains(w.Body.String(), "Your message has been submitted.") {
			t.Fatalf("replacement contact write failed: %d %s", w.Code, w.Body.String())
		}

		contacts.fail = errors.New("private contact failure")
		w = h.request(http.MethodPost, "/contact", form, htmx)
		if len(contacts.submissions) != 1 || !strings.Contains(w.Body.String(), "Unable to save your message") || strings.Contains(w.Body.String(), "private contact failure") {
			t.Fatal("replacement contact error was hidden or exposed")
		}
		accounts.fail = errors.New("private account failure")
		w = h.request(http.MethodGet, "/", nil, htmx)
		if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "private account failure") {
			t.Fatal("replacement account error did not remain an internal failure")
		}
		accounts.fail = nil
		sessions.fail = errors.New("private session failure")
		w = h.request(http.MethodGet, "/", nil, htmx)
		if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "private session failure") {
			t.Fatal("replacement session error was hidden or exposed")
		}
		sessions.fail = nil
		origin, err := url.Parse(h.baseURL)
		if err != nil {
			t.Fatal(err)
		}
		oldCookies := h.cookies.Cookies(origin)
		w = h.request(http.MethodPost, "/auth/logout", nil, htmx)
		if w.Header().Get("HX-Redirect") != "/" && w.Header().Get("Location") != "/" {
			t.Fatalf("replacement logout failed: %d %s", w.Code, w.Body.String())
		}
		before := len(h.mail.snapshot())
		w = h.request(http.MethodPost, "/test-email?to=recipient@example.invalid", nil, htmx)
		if w.Code != http.StatusForbidden || len(h.mail.snapshot()) != before {
			t.Fatal("logout retained access to authenticated mail delivery")
		}
		h.cookies.SetCookies(origin, oldCookies)
		w = h.request(http.MethodPost, "/test-email?to=recipient@example.invalid", nil, htmx)
		if w.Code != http.StatusForbidden || len(h.mail.snapshot()) != before {
			t.Fatal("replacement session provider accepted a logged-out cookie")
		}

		h.app.OnShutdown("consumer", func(context.Context) error {
			if sessions.closes != 0 {
				t.Error("shared sessions closed before their consumer")
			}
			return nil
		})
		for range 2 {
			if err := h.app.Shutdown(); err != nil {
				t.Fatal(err)
			}
		}
		if sessions.closes != 0 || h.app.contactDB != nil {
			t.Fatal("incorrect provider ownership or unexpected SQL setup")
		}
	}
	if entries, err := os.ReadDir(directory); err != nil || len(entries) != 0 {
		t.Fatalf("replacement setup created local files: %v %v", entries, err)
	}
}

// This small in-memory test store exercises session replacement through real HTTP
// flows. It is not a supported provider or a production implementation.
type replacementSessionStore struct {
	mu        sync.Mutex
	records   map[string]replacementSessionRecord
	rotations int
	closes    int
	fail      error
}

type replacementSessionRecord struct {
	name, data string
	expires    time.Time
}

func (*replacementSessionStore) IsEnabled() bool { return true }

func (s *replacementSessionStore) New(r *http.Request, name string) (*session.Session, error) {
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	return session.NewSession(s, name), nil
}

func (s *replacementSessionStore) Get(r *http.Request, name string) (*session.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	if s.closes != 0 {
		return nil, errors.New("test session store closed")
	}
	if s.fail != nil {
		return nil, s.fail
	}
	cookie, err := r.Cookie(name)
	if err == nil {
		if _, err := uuid.Parse(cookie.Value); err != nil {
			return nil, hs_errors.WithCause(session.ErrInvalidToken, err)
		}
		record, ok := s.records[cookie.Value]
		if ok && record.name == name && time.Now().Before(record.expires) {
			return session.LoadSession(cookie.Value, name, record.expires, s, record.data, nil)
		}
	}
	return s.New(r, name)
}

func (s *replacementSessionStore) Save(w http.ResponseWriter, r *http.Request, value *session.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.save(w, r, value)
}

func (s *replacementSessionStore) save(w http.ResponseWriter, r *http.Request, value *session.Session) error {
	if err := r.Context().Err(); err != nil {
		return err
	}
	if s.closes != 0 {
		return errors.New("test session store closed")
	}
	encoded, err := value.EncodedData(nil)
	if err != nil {
		return err
	}
	cookie := &http.Cookie{Name: value.Name, Value: value.ID, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode}
	if err := cookie.Valid(); err != nil {
		return err
	}
	if len(cookie.String()) > session.MaxCookieSize {
		return session.ErrSessionCookieTooLarge
	}

	stored, exists := s.records[value.ID]
	expires := stored.expires
	if value.IsNew {
		if exists {
			return hs_errors.WithCause(session.ErrSessionInvalid, errors.New("session ID already exists"))
		}
		expires = time.Now().Add(time.Hour)
	} else if !exists {
		return hs_errors.WithCause(session.ErrSessionInvalid, errors.New("session was revoked"))
	}

	s.records[value.ID] = replacementSessionRecord{name: value.Name, data: encoded, expires: expires}
	value.ExpiresAt = expires
	value.IsNew = false
	http.SetCookie(w, cookie)
	return nil
}

func (s *replacementSessionStore) Rotate(w http.ResponseWriter, r *http.Request, old *session.Session, data map[string]any) (*session.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	if !old.IsNew {
		stored, exists := s.records[old.ID]
		if !exists || !time.Now().Before(stored.expires) {
			return nil, session.ErrSessionInvalid
		}
	}

	replacement := session.NewSession(s, old.Name)
	replacement.Data = data
	if err := s.save(w, r, replacement); err != nil {
		return nil, err
	}
	delete(s.records, old.ID)
	s.rotations++
	return replacement, nil
}

func (s *replacementSessionStore) End(w http.ResponseWriter, r *http.Request, value *session.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := r.Context().Err(); err != nil {
		return err
	}
	delete(s.records, value.ID)
	session.ExpireCookie(w, r, value.Name)
	return nil
}

func (s *replacementSessionStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closes++
	return nil
}

func TestMemorySessionContract(t *testing.T) {
	sessiontest.Run(t, func(t *testing.T) session.SessionStore {
		store := &replacementSessionStore{records: make(map[string]replacementSessionRecord)}
		t.Cleanup(func() { store.Close() })
		return store
	})
}

func TestMemorySessionExpiryBoundary(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := &replacementSessionStore{records: make(map[string]replacementSessionRecord)}
		t.Cleanup(func() { store.Close() })
		s := session.NewSession(store, "account")
		s.Data["identity"] = "account"
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if err := store.Save(httptest.NewRecorder(), r, s); err != nil {
			t.Fatal(err)
		}
		r.AddCookie(&http.Cookie{Name: s.Name, Value: s.ID})

		time.Sleep(time.Until(s.ExpiresAt))
		expired, err := store.Get(r, s.Name)
		if err != nil || expired == nil || !expired.IsNew || len(expired.Data) != 0 {
			t.Fatalf("expired session restored identity: %+v, %v", expired, err)
		}
		w := httptest.NewRecorder()
		if next, err := store.Rotate(w, r, s, s.Data); next != nil || err == nil {
			t.Fatalf("expired rotation = %+v, %v", next, err)
		}
		if len(w.Header().Values("Set-Cookie")) != 0 {
			t.Fatal("expired rotation issued a cookie")
		}
	})
}
