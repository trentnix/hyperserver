package auth

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/trentnix/hyperserver/pkg/server"
	contentservices "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

// These providers intentionally implement no unrelated workflow methods.
type capabilityProvider struct {
	name  string
	calls int
}

func (p *capabilityProvider) AuthType() string { return p.name }
func (p *capabilityProvider) Init(*server.ApplicationServer) error {
	return nil
}
func (p *capabilityProvider) respond(w http.ResponseWriter) {
	p.calls++
	w.WriteHeader(http.StatusNoContent)
}

type loginOnlyProvider struct{ capabilityProvider }

func (*loginOnlyProvider) GetLoginButton() template.HTML { return "login-choice" }
func (p *loginOnlyProvider) GetLogin(w http.ResponseWriter, _ *http.Request) {
	p.respond(w)
}
func (p *loginOnlyProvider) Login(w http.ResponseWriter, _ *http.Request) *user.User {
	p.respond(w)
	return nil // Provider owns the response when no account is returned.
}

type registrationOnlyProvider struct{ capabilityProvider }

func (*registrationOnlyProvider) GetRegisterButton() template.HTML { return "register-choice" }
func (p *registrationOnlyProvider) GetRegister(w http.ResponseWriter, _ *http.Request) {
	p.respond(w)
}
func (p *registrationOnlyProvider) Register(w http.ResponseWriter, _ *http.Request) {
	p.respond(w)
}

type resetOnlyProvider struct{ capabilityProvider }

func (p *resetOnlyProvider) GetResetRequest(w http.ResponseWriter, _ *http.Request) {
	p.respond(w)
}
func (p *resetOnlyProvider) ResetRequest(w http.ResponseWriter, _ *http.Request, _ time.Duration) {
	p.respond(w)
}
func (p *resetOnlyProvider) GetReset(w http.ResponseWriter, _ *http.Request, _ string) {
	p.respond(w)
}
func (p *resetOnlyProvider) Reset(w http.ResponseWriter, _ *http.Request, _ *user.User, _ string, _ bool) {
	p.respond(w)
}

type changeOnlyProvider struct{ capabilityProvider }

func (p *changeOnlyProvider) GetChange(w http.ResponseWriter, _ *http.Request) {
	p.respond(w)
}
func (p *changeOnlyProvider) Change(w http.ResponseWriter, _ *http.Request, _ *user.User) {
	p.respond(w)
}

func TestUnsupportedProviderWorkflows(t *testing.T) {
	provider := &loginOnlyProvider{capabilityProvider: capabilityProvider{name: "login-only"}}
	noWorkflows := &capabilityProvider{name: "no-workflows"}
	manager := &AuthManager{
		registrationEnabled: true,
		Services:            &Registry{services: []AuthService{provider, noWorkflows}},
	}
	for _, htmx := range []bool{false, true} {
		for _, name := range []string{"login-only", "unknown", "no-workflows"} {
			for _, route := range []struct {
				method, path string
				handler      http.HandlerFunc
			}{
				{http.MethodGet, "/auth/login/", manager.GetLoginService},
				{http.MethodPost, "/auth/login/", manager.Login},
				{http.MethodGet, "/auth/register/", manager.GetRegisterService},
				{http.MethodPost, "/auth/register/", manager.Register},
				{http.MethodGet, "/auth/reset/request/", manager.GetResetRequest},
				{http.MethodPost, "/auth/reset/request/", manager.ResetRequest},
				{http.MethodGet, "/auth/reset/", manager.GetReset},
				{http.MethodPost, "/auth/reset/", manager.Reset},
				{http.MethodGet, "/auth/change/", manager.GetChange},
				{http.MethodPost, "/auth/change/", manager.Change},
			} {
				if name == "login-only" && route.path == "/auth/login/" {
					continue
				}
				r := httptest.NewRequest(route.method, route.path+name+"?token=unused", nil)
				r.SetPathValue("authType", name)
				if htmx {
					r.Header.Set("HX-Request", "true")
				}
				w := httptest.NewRecorder()
				route.handler(w, r)
				if w.Code != http.StatusNotFound {
					t.Errorf("%s %s (HTMX=%t): status %d, want 404", route.method, r.URL.Path, htmx, w.Code)
				}
			}
		}
	}
	if provider.calls != 0 {
		t.Fatal("unsupported workflow called a provider")
	}
	// No account repository was supplied. Unsupported reset routes must return
	// before looking up tokens or touching account storage.
}

func TestProviderCapabilityDispatch(t *testing.T) {
	base := capabilityProvider{name: "custom"}
	login := &loginOnlyProvider{base}
	registration := &registrationOnlyProvider{base}
	reset := &resetOnlyProvider{base}
	change := &changeOnlyProvider{base}
	manager := &AuthManager{registrationEnabled: true}
	for _, tc := range []struct {
		name     string
		provider AuthService
		handler  http.HandlerFunc
	}{
		{"login form", login, manager.GetLoginService},
		{"login", login, manager.Login},
		{"registration form", registration, manager.GetRegisterService},
		{"registration", registration, manager.Register},
		{"reset request form", reset, manager.GetResetRequest},
		{"reset request", reset, manager.ResetRequest},
		{"change form", change, manager.GetChange},
		{"change", change, manager.Change},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager.Services = &Registry{services: []AuthService{tc.provider}}
			r := httptest.NewRequest(http.MethodPost, "/", nil)
			r.SetPathValue("authType", "custom")
			r = user.AddUserToRequestContext(r, &user.User{ID: "account"})
			w := httptest.NewRecorder()
			tc.handler(w, r)
			if w.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want provider response", w.Code)
			}
		})
	}
	if login.calls != 2 || registration.calls != 2 || reset.calls != 2 || change.calls != 2 {
		t.Fatal("a workflow did not reach its provider")
	}
}

func TestProviderChoicesMatchCapabilities(t *testing.T) {
	login := &loginOnlyProvider{capabilityProvider: capabilityProvider{name: "login-only"}}
	otherLogin := &loginOnlyProvider{capabilityProvider: capabilityProvider{name: "other-login"}}
	registration := &registrationOnlyProvider{capabilityProvider: capabilityProvider{name: "registration-only"}}
	otherRegistration := &registrationOnlyProvider{capabilityProvider: capabilityProvider{name: "other-registration"}}
	for _, tc := range []struct {
		name         string
		providers    []AuthService
		registration bool
		wantStatus   int
		want         string
	}{
		{"single login", []AuthService{login, registration}, false, 200, `hx-get="/auth/login/login-only"`},
		{"multiple logins", []AuthService{login, otherLogin, registration}, false, 200, "login-choice"},
		{"no login", []AuthService{registration}, false, 404, "404 page not found"},
		{"single registration", []AuthService{login, registration}, true, 200, `hx-get="/auth/register/registration-only"`},
		{"multiple registrations", []AuthService{login, registration, otherRegistration}, true, 200, "register-choice"},
		{"no registration", []AuthService{login}, true, 403, "Registration is not available."},
		{"no providers", nil, true, 403, "Registration is not available."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cm := contentservices.NewContentManager()
			layouts := fstest.MapFS{"layout.html": &fstest.MapFile{Data: []byte(`{{renderPartial .PartialName .}}`)}}
			cm.AddPageLayout(layouts, "layout.html")
			cm.AddHtmxLayout(layouts, "layout.html")
			manager := &AuthManager{registrationEnabled: true, contentManager: cm, Services: &Registry{services: tc.providers}}
			if err := manager.prepareTemplates(); err != nil {
				t.Fatal(err)
			}
			for _, htmx := range []bool{false, true} {
				r := httptest.NewRequest(http.MethodGet, "/", nil)
				if htmx {
					r.Header.Set("HX-Request", "true")
				}
				w := httptest.NewRecorder()
				if tc.registration {
					manager.GetRegister(w, r)
				} else {
					manager.GetLogin(w, r)
				}
				if w.Code != tc.wantStatus || !strings.Contains(w.Body.String(), tc.want) {
					t.Fatalf("HTMX=%t: response = %d %s, want %d %q", htmx, w.Code, w.Body.String(), tc.wantStatus, tc.want)
				}
				wrongChoice := "register-choice"
				if tc.registration {
					wrongChoice = "login-choice"
				}
				if strings.Contains(w.Body.String(), wrongChoice) {
					t.Fatal("selection included an unsupported workflow")
				}
			}
		})
	}
}
