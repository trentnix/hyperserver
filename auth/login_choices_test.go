package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	contentservices "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/flash"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

type availableLoginProvider struct {
	loginOnlyProvider
	available bool
	err       error
	account   *user.User
	ctx       context.Context
}

type failingLoginMessages struct{ session.SessionStore }

func (failingLoginMessages) Save(http.ResponseWriter, *http.Request, *session.Session) error {
	return errors.New("private message storage failure")
}

func (failingLoginMessages) End(http.ResponseWriter, *http.Request, *session.Session) error {
	return errors.New("private message storage failure")
}

func TestSharedLoginReportsMessageRemovalFailure(t *testing.T) {
	for _, operation := range []string{"save", "end"} {
		for _, hx := range []bool{false, true} {
			manager, _, sessions, _ := newLookupTestManager(t)
			manager.Services = &Registry{services: []AuthService{&loginOnlyProvider{}}}
			r := session.AddSessionManagerToRequestContext(httptest.NewRequest("GET", "/login", nil), sessions)
			if hx {
				r.Header.Set("HX-Request", "true")
			}
			if err := flash.AddMessage(httptest.NewRecorder(), r, "pending", flash.AuthMessages); err != nil {
				t.Fatal(err)
			}
			if operation == "save" {
				if err := flash.AddSuccessNotification(httptest.NewRecorder(), r, "other"); err != nil {
					t.Fatal(err)
				}
			}
			s, err := session.Get(r, "hs-message-session")
			if err != nil {
				t.Fatal(err)
			}
			s.Store = failingLoginMessages{s.Store}
			w := httptest.NewRecorder()
			manager.GetLogin(w, r)
			if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "Unable to load authentication messages.") || strings.Contains(w.Body.String(), "private") {
				t.Fatalf("%s HTMX=%t: %d %s", operation, hx, w.Code, w.Body)
			}
		}
	}
}

func (p *availableLoginProvider) LoginAvailable(ctx context.Context, account *user.User) (bool, error) {
	p.ctx, p.account = ctx, account
	return p.available, p.err
}

func TestLoginChoicesPolicy(t *testing.T) {
	account := &user.User{ID: "owner"}
	for _, tc := range []struct {
		name      string
		account   *user.User
		available bool
		want      int
	}{
		{"no account restriction", nil, false, 2},
		{"available method", account, true, 2},
		{"unavailable method", account, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := &availableLoginProvider{available: tc.available}
			unknown := &loginOnlyProvider{}
			manager := &AuthManager{
				Services: &Registry{services: []AuthService{provider, unknown, &registrationOnlyProvider{}}},
				Policy: Policy{LoginAccount: func(ctx context.Context) (*user.User, error) {
					if ctx != t.Context() {
						t.Fatal("policy lost the request context")
					}
					return tc.account, nil
				}},
			}
			choices, err := manager.loginChoices(t.Context())
			if err != nil || len(choices) != tc.want || choices[len(choices)-1] != unknown {
				t.Fatalf("choices = %v, %v", choices, err)
			}
			if provider.account != tc.account || (tc.account != nil && provider.ctx != t.Context()) {
				t.Fatal("provider did not receive the selected account and request context")
			}
		})
	}
}

func TestLoginChoicesErrors(t *testing.T) {
	for _, kind := range []string{"account lookup", "provider lookup", "no available method"} {
		t.Run(kind, func(t *testing.T) {
			provider := &availableLoginProvider{}
			manager := &AuthManager{
				Services:       &Registry{services: []AuthService{provider}},
				contentManager: contentservices.NewContentManager(),
				Policy: Policy{LoginAccount: func(context.Context) (*user.User, error) {
					if kind == "account lookup" {
						return nil, errors.New("account storage failed")
					}
					return &user.User{ID: "owner"}, nil
				}},
			}
			manager.contentManager.HandleError = func(w http.ResponseWriter, r *http.Request, message string, _ error, status int) {
				http.Error(w, message, status)
			}
			provider.name = "test"
			manager.contentManager.AddPageLayout(fstest.MapFS{"layout": {Data: []byte(`{{renderPartial .PartialName .}}`)}}, "layout")
			manager.contentManager.AddHtmxLayout(fstest.MapFS{"layout": {Data: []byte(`{{renderPartial .PartialName .}}`)}}, "layout")
			if err := manager.prepareTemplates(); err != nil {
				t.Fatal(err)
			}
			_, _, sessions, _ := newLookupTestManager(t)
			if kind == "provider lookup" {
				provider.err = errors.New("identity storage failed")
			}
			for _, hx := range []bool{false, true} {
				for _, route := range []struct {
					method  string
					handler http.HandlerFunc
					direct  bool
				}{
					{"GET", manager.GetLogin, false},
					{"GET", manager.GetLoginService, true},
					{"POST", manager.Login, true},
				} {
					provider.calls = 0
					r := httptest.NewRequest(route.method, "/login", strings.NewReader("provider=test"))
					r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					r = session.AddSessionManagerToRequestContext(r, sessions)
					r.SetPathValue("authType", "test")
					if hx {
						r.Header.Set("HX-Request", "true")
					}
					w := httptest.NewRecorder()
					route.handler(w, r)
					status, calls := http.StatusServiceUnavailable, 0
					if route.method == "POST" {
						status, calls = http.StatusNoContent, 1
					} else if route.direct {
						status = http.StatusSeeOther
						if hx {
							status = http.StatusOK
						}
					} else if kind == "provider lookup" {
						status = http.StatusOK
					}
					if w.Code != status || provider.calls != calls {
						t.Fatalf("failed lookup: status=%d, provider calls=%d", w.Code, provider.calls)
					}
				}
			}
		})
	}
}

func TestHealthyLoginSurvivesAlternativeFailure(t *testing.T) {
	healthy := &loginOnlyProvider{capabilityProvider: capabilityProvider{name: "healthy"}}
	broken := &availableLoginProvider{err: errors.New("alternative lookup unavailable")}
	broken.name = "alternative"
	manager := &AuthManager{
		Services: &Registry{services: []AuthService{healthy, broken}},
		Policy: Policy{LoginAccount: func(context.Context) (*user.User, error) {
			return &user.User{ID: "owner"}, nil
		}},
	}
	for _, hx := range []bool{false, true} {
		r := httptest.NewRequest("POST", "/login", strings.NewReader("provider=healthy"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.SetPathValue("authType", "healthy")
		if hx {
			r.Header.Set("HX-Request", "true")
		}
		w := httptest.NewRecorder()
		healthy.calls = 0
		manager.Login(w, r)
		if healthy.calls != 1 || w.Code != http.StatusNoContent {
			t.Fatalf("healthy provider calls=%d, HTTP status=%d", healthy.calls, w.Code)
		}
	}
}
