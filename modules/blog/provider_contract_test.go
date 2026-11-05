package blog

import (
	"context"
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/auth"
	authmiddleware "github.com/trentnix/hyperserver/auth/middleware"
	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

// This test-only provider stands in for a protocol adapter after proof validation.
// It uses the same public integration points as an imported third-party package.
// No production blog code recognizes its name, routes, or identity namespace.
func init() {
	auth.Register(auth.Descriptor{Name: "test-external", New: func(d auth.Dependencies) auth.AuthService {
		return &contractAuthProvider{dependencies: d}
	}})
}

type contractAuthProvider struct {
	dependencies         auth.Dependencies
	accounts             auth.ExternalAccounts
	view                 *content.View
	app                  *server.ApplicationServer
	email                string
	availabilityFailure  bool
	signInOnRegistration bool
}

func (*contractAuthProvider) AuthType() string            { return "test-external" }
func (*contractAuthProvider) ValidateVerification() error { return nil }
func (p *contractAuthProvider) Init(app *server.ApplicationServer) error {
	p.app = app
	options := auth.GetAuthConfigOptions(app.Config, p.AuthType())
	p.email, p.availabilityFailure = options["email"], options["availabilityFailure"] == "true"
	p.signInOnRegistration = options["signInOnRegistration"] == "true"
	p.accounts = auth.ExternalAccounts{Identities: p.dependencies.Accounts.(user.ExternalIdentityRepository), Policy: p.dependencies.Policy, Content: app.ContentManager, Provider: p.AuthType(), RegistrationEnabled: app.Config.Auth.RegistrationEnabled}
	var err error
	p.view, err = auth.PrepareProviderView(app.ContentManager)
	return err
}
func (*contractAuthProvider) GetLoginButton() template.HTML {
	return `<form method="post" action="/login" hx-boost="false" class="auth-provider-button"><input type="hidden" name="provider" value="test-external"><button>Sign in with Example</button></form>`
}
func (*contractAuthProvider) GetRegisterButton() template.HTML {
	return `<form method="post" action="/register" hx-boost="false" class="auth-provider-button"><input type="hidden" name="provider" value="test-external"><button>Register with Example</button></form>`
}
func (p *contractAuthProvider) render(w http.ResponseWriter, r *http.Request, page auth.ProviderPage) {
	c := p.view.Content(r, page)
	if err := c.Render(w, r); err != nil {
		c.HandleRenderError(w, r, "Unable to display test provider", err)
	}
}
func (p *contractAuthProvider) Login(w http.ResponseWriter, r *http.Request) auth.LoginResult {
	p.start(w, r, "login")
	return auth.LoginResult{}
}
func (p *contractAuthProvider) Register(w http.ResponseWriter, r *http.Request) auth.RegistrationResult {
	p.start(w, r, "register")
	return auth.RegistrationResult{}
}
func (p *contractAuthProvider) LoginAvailable(ctx context.Context, account *user.User) (bool, error) {
	if p.availabilityFailure {
		return false, errors.New("test provider lookup unavailable")
	}
	return p.accounts.Identities.HasExternalIdentity(ctx, account.ID, "https://identity.example")
}
func (p *contractAuthProvider) AccountMethod(ctx context.Context, u *user.User) (auth.AccountMethod, error) {
	connected, err := p.LoginAvailable(ctx, u)
	return auth.AccountMethod{Name: "Example", Connected: connected, Actions: []auth.AccountAction{{Label: "Example login", URL: "/auth/link/test-external"}}}, err
}
func (*contractAuthProvider) IdentityIssuer() string { return "https://identity.example" }
func (p *contractAuthProvider) Routes(routes, mutations *routing.Routes) error {
	protect := authmiddleware.RequireAuthentication(p.dependencies.Accounts, p.app.ContentManager)
	routes.Handle("GET /auth/link/test-external", protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.render(w, r, auth.ProviderPage{Title: "Connect Example", Controls: []template.HTML{`<form method="post" action="/auth/link/test-external" hx-boost="false"><button>Connect Example</button></form>`}})
	})))
	mutations.Handle("POST /auth/link/test-external", protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { p.start(w, r, "link") })))
	return routes.Err()
}
func (p *contractAuthProvider) start(w http.ResponseWriter, r *http.Request, mode string) {
	request := auth.ExternalAccountRequest{Mode: mode, Identity: user.ExternalIdentity{Issuer: "https://identity.example", Subject: "example-user"}}
	if mode == "register" {
		request.VerifiedEmail = p.email
	}
	if current := user.GetUserFromContext(r.Context()); mode == "link" && current != nil {
		request.AccountID, request.SessionVersion = current.ID, current.SessionVersion
	}
	s, err := session.Get(r, "test-external-flow")
	if err == nil {
		err = s.Rotate(w, r, map[string]any{"request": request})
	}
	if err != nil {
		http.Error(w, "flow unavailable", 503)
		return
	}
	http.Redirect(w, r, "/auth/callback/test-external", http.StatusSeeOther)
}
func (p *contractAuthProvider) Callback(w http.ResponseWriter, r *http.Request) *user.User {
	s, err := session.Get(r, "test-external-flow")
	var request auth.ExternalAccountRequest
	if err == nil {
		err = s.DecodeValue("request", &request)
	}
	if err != nil || request.Mode == "" {
		http.Error(w, "invalid flow", 400)
		return nil
	}
	if err := s.Rotate(w, r, map[string]any{}); err != nil {
		http.Error(w, "used flow", 400)
		return nil
	}
	account, created := p.accounts.Resolve(w, r, request)
	if created && !p.signInOnRegistration {
		p.render(w, r, auth.ProviderPage{Title: "Account ready", Message: "Sign in to continue."})
		return nil
	}
	return account
}

func thirdProviderHarness(t *testing.T, email string, failure bool) *harness {
	cfg := harnessConfig(t, 10)
	cfg.HTTP.FormActionOrigins = []string{"https://identity.example"}
	options := map[string]string{"enabled": "true", "email": email}
	if failure {
		options["availabilityFailure"] = "true"
	}
	cfg.Auth.Services["test-external"] = options
	return startHarness(t, cfg)
}

func completeThirdProviderFlow(t *testing.T, h *harness, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := h.request("POST", path, url.Values{"provider": {"test-external"}}, false)
	requireStatus(t, w, http.StatusSeeOther)
	return h.request("GET", w.Header().Get("Location"), nil, false)
}

func TestThirdProviderRegistrationAndLogin(t *testing.T) {
	h := thirdProviderHarness(t, "trentnix@gmail.com", false)
	for _, hx := range []bool{false, true} {
		w := h.request("GET", "/register", nil, hx)
		requireStatus(t, w, 200)
		if !strings.Contains(w.Body.String(), "Register with Example") || !hx && !strings.Contains(w.Body.String(), "Set up your blog") {
			t.Fatal("new provider lost registration choices or onboarding", w.Body)
		}
	}
	w := completeThirdProviderFlow(t, h, "/register")
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "Account ready") || h.cookies["auth-user-session"] != nil {
		t.Fatal("registration must complete without login", w.Body)
	}
	account, err := h.app.Accounts.GetByEmail(t.Context(), "trentnix@gmail.com")
	if err != nil || account.NeedsVerification() || account.Password != "" {
		t.Fatal("incorrect external account", err)
	}
	requireStatus(t, h.request("GET", "/", nil, false), 200)
	w = h.request("GET", "/login", nil, false)
	if strings.Contains(w.Body.String(), `name="password"`) || !strings.Contains(w.Body.String(), "Sign in with Example") || !strings.Contains(w.Body.String(), "Sign in to your blog") {
		t.Fatal("single-account choices or onboarding incorrect", w.Body)
	}
	policy := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(policy, "form-action 'self' https://identity.example") || strings.Contains(policy, "accounts.google.com") {
		t.Fatal("provider-specific CSP", policy)
	}
	w = completeThirdProviderFlow(t, h, "/login")
	requireStatus(t, w, 303)
	if h.cookies[returningUserCookie] == nil {
		t.Fatal("shared after-login policy was skipped")
	}
	w = h.request("GET", w.Header().Get("Location"), nil, false)
	if w.Header().Get("Location") != "/profile" {
		t.Fatal("first login did not open profile", w.Header())
	}
	requireStatus(t, h.request("GET", "/auth/account", nil, false), 200)
	requireStatus(t, h.request("POST", "/auth/logout", nil, false), 303)
	for _, hx := range []bool{false, true} {
		w = h.request("GET", "/login", nil, hx)
		requireStatus(t, w, 200)
		if !strings.Contains(w.Body.String(), "Welcome back") || strings.Contains(w.Body.String(), `class="header-sign-in"`) || strings.Contains(w.Body.String(), "reader-search-form") {
			t.Fatal("provider-neutral presentation missing", w.Body)
		}
	}
}

// Google and other providers can complete registration and login in one callback.
// The blog must apply its onboarding and returning-user behavior without knowing
// which provider authenticated the account.
func TestRegistrationLoginOpensSiteProfile(t *testing.T) {
	cfg := harnessConfig(t, 10)
	cfg.Auth.Services["test-external"] = map[string]string{
		"enabled": "true", "email": "trentnix@gmail.com", "signInOnRegistration": "true",
	}
	h := startHarness(t, cfg)
	w := completeThirdProviderFlow(t, h, "/register")
	requireStatus(t, w, http.StatusSeeOther)
	if w.Header().Get("Location") != authReturnURL || h.cookies["auth-user-session"] == nil || h.cookies[returningUserCookie] == nil {
		t.Fatal("registration skipped the normal login flow", w.Header())
	}
	w = h.request("GET", w.Header().Get("Location"), nil, false)
	requireStatus(t, w, http.StatusSeeOther)
	if w.Header().Get("Location") != "/profile" || !h.blog.access.setup.complete.Load() {
		t.Fatal("registration login did not finish onboarding", w.Header())
	}
	for _, hx := range []bool{false, true} {
		w = h.request("GET", "/profile", nil, hx)
		requireStatus(t, w, http.StatusOK)
		if !strings.Contains(w.Body.String(), "Give your blog a name and a short introduction.") {
			t.Fatal("new administrator did not receive profile guidance")
		}
	}
}

func TestThirdProviderLinkingAndFailureIsolation(t *testing.T) {
	for _, failure := range []bool{false, true} {
		h := thirdProviderHarness(t, "", failure)
		h.register(t)
		h.verify(t)
		// This real password login must work even if the other provider's
		// availability lookup fails. No external email claim is needed to link.
		h.login(t)
		account, err := h.app.Accounts.GetByEmail(t.Context(), "trentnix@gmail.com")
		if err != nil {
			t.Fatal(err)
		}
		w := h.request("GET", "/auth/account", nil, false)
		if failure {
			// Account status requires a successful lookup. Login and the
			// provider's explicit linking route must still work independently.
			requireStatus(t, w, http.StatusServiceUnavailable)
		} else if !strings.Contains(w.Body.String(), `href="/auth/link/test-external"`) {
			t.Fatal("account action missing")
		}
		requireStatus(t, h.request("GET", "/auth/link/test-external", nil, false), 200)
		requireStatus(t, completeThirdProviderFlow(t, h, "/auth/link/test-external"), 303)
		linked, err := h.app.Accounts.(user.ExternalIdentityRepository).GetExternalAccount(t.Context(), user.ExternalIdentity{Issuer: "https://identity.example", Subject: "example-user"})
		if err != nil || linked.ID != account.ID || linked.Password != account.Password || linked.SessionVersion != account.SessionVersion+1 {
			t.Fatal("link changed credentials or failed to revoke sessions", err)
		}
		requireStatus(t, h.request("POST", "/auth/logout", nil, false), 303)
		w = h.request("GET", "/login", nil, false)
		if !strings.Contains(w.Body.String(), `name="password"`) || !strings.Contains(w.Body.String(), "Sign in with Example") {
			t.Fatal("linked methods missing")
		}
		requireStatus(t, completeThirdProviderFlow(t, h, "/login"), 303)
	}
}

func TestThirdProviderAlongsideGoogle(t *testing.T) {
	cfg := harnessConfig(t, 10)
	cfg.Auth.Services["email"]["enabled"] = "false"
	cfg.Auth.Services["google"] = map[string]string{"enabled": "true", "clientID": "test-client", "clientSecret": "test-secret"}
	cfg.Auth.Services["test-external"] = map[string]string{"enabled": "true"}
	h := startHarness(t, cfg)
	for _, hx := range []bool{false, true} {
		w := h.request("GET", "/register", nil, hx)
		requireStatus(t, w, 200)
		if !strings.Contains(w.Body.String(), "Sign up with Google") || !strings.Contains(w.Body.String(), "Register with Example") {
			t.Fatal("external registration choices missing", w.Body)
		}
	}
	account := &user.User{Email: h.blog.access.setup.email, Verified: true}
	repository := h.app.Accounts.(user.ExternalIdentityRepository)
	if err := repository.CreateExternalAccount(t.Context(), account, user.ExternalIdentity{Issuer: "https://identity.example", Subject: "example-user"}); err != nil {
		t.Fatal(err)
	}
	if err := repository.LinkExternalIdentity(t.Context(), account, user.ExternalIdentity{Issuer: "https://accounts.google.com", Subject: "owner"}); err != nil {
		t.Fatal(err)
	}
	for _, hx := range []bool{false, true} {
		w := h.request("GET", "/login", nil, hx)
		requireStatus(t, w, 200)
		if !strings.Contains(w.Body.String(), `class="auth-provider"`) || !strings.Contains(w.Body.String(), "Sign in with Google") || !strings.Contains(w.Body.String(), "Sign in with Example") || strings.Contains(w.Body.String(), `name="password"`) {
			t.Fatal("external login choices lost shared presentation", w.Body)
		}
	}
}

func TestThirdProviderAdmissionAndRegistrationClosure(t *testing.T) {
	for _, closed := range []bool{false, true} {
		email := "not-the-admin@example.com"
		if closed {
			email = "trentnix@gmail.com"
		}
		h := thirdProviderHarness(t, email, false)
		w := h.request("POST", "/register", url.Values{"provider": {"test-external"}}, false)
		requireStatus(t, w, 303)
		if closed {
			h.register(t)
		}
		w = h.request("GET", w.Header().Get("Location"), nil, false)
		if closed {
			requireStatus(t, w, 303)
		} else {
			requireStatus(t, w, 403)
		}
		_, err := h.app.Accounts.(user.ExternalIdentityRepository).GetExternalAccount(t.Context(), user.ExternalIdentity{Issuer: "https://identity.example", Subject: "example-user"})
		if !errors.Is(err, user.ErrUserNotFound) || h.cookies["auth-user-session"] != nil {
			t.Fatal("rejected registration changed accounts", err)
		}
	}
}
