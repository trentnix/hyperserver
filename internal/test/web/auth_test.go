package web_test

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

// This fixture tests provider composition, not credential validation. It implements
// only login and lifecycle methods, with no embedded interfaces or route hook.
type loginOnlyHTTPProvider struct {
	accounts user.AccountRepository
}

var (
	_ auth.AuthService  = (*loginOnlyHTTPProvider)(nil)
	_ auth.LoginHandler = (*loginOnlyHTTPProvider)(nil)
)

func (*loginOnlyHTTPProvider) AuthType() string { return "login-only" }

func (p *loginOnlyHTTPProvider) Init(*server.ApplicationServer) error {
	return nil
}

func (*loginOnlyHTTPProvider) GetLoginButton() template.HTML {
	return `<a href="/auth/login/login-only">Test login</a>`
}

func (*loginOnlyHTTPProvider) GetLogin(w http.ResponseWriter, _ *http.Request) {
	fmt.Fprint(w, "Login-only provider form")
}

func (p *loginOnlyHTTPProvider) Login(w http.ResponseWriter, r *http.Request) *user.User {
	if r.FormValue("credential") != "test-only-credential" {
		http.Error(w, "Test credential rejected", http.StatusUnauthorized)
		return nil
	}
	account, err := p.accounts.GetByEmail(r.Context(), "login-only@example.invalid")
	if err != nil {
		http.Error(w, "Test account unavailable", http.StatusInternalServerError)
		return nil
	}
	return account
}

func TestHTTPLoginOnlyProvider(t *testing.T) {
	for _, htmx := range []bool{false, true} {
		t.Run(fmt.Sprintf("htmx=%t", htmx), func(t *testing.T) {
			catalog := func(h *httpHarness) ([]handlers.Descriptor, error) {
				modules, err := siteCatalog(h, "")
				if err != nil {
					return nil, err
				}
				providers := []auth.Descriptor{{Name: "login-only", New: func() auth.AuthService {
					return &loginOnlyHTTPProvider{accounts: h.accounts}
				}}}
				return handlers.Replace(modules, auth.Module(h.accounts, h.sessions, providers))
			}
			runSiteScenarioWithCatalog(t, catalog, func(h *httpHarness) {
				account := &user.User{Email: "login-only@example.invalid", RegistrationAuthType: "login-only", Verified: true}
				if err := h.accounts.Create(t.Context(), account); err != nil {
					t.Fatal(err)
				}

				w := h.request(http.MethodGet, "/auth/login", nil, htmx)
				if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `hx-get="/auth/login/login-only"`) {
					t.Fatalf("login selection = %d %s", w.Code, w.Body.String())
				}
				w = h.request(http.MethodGet, "/auth/login/login-only", nil, htmx)
				if w.Code != http.StatusOK || w.Body.String() != "Login-only provider form" {
					t.Fatalf("login form = %d %s", w.Code, w.Body.String())
				}
				w = h.request(http.MethodGet, "/auth/register", nil, htmx)
				if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "Registration is not available.") {
					t.Fatalf("registration selection = %d %s", w.Code, w.Body.String())
				}
				for _, path := range []string{
					"/auth/register/login-only",
					"/auth/reset/request/login-only",
					"/auth/reset/login-only?token=unused",
				} {
					for _, method := range []string{http.MethodGet, http.MethodPost} {
						w = h.request(method, path, nil, htmx)
						if w.Code != http.StatusNotFound || w.Result().Header.Get("Cache-Control") != "no-store" {
							t.Fatalf("unsupported %s %s: status=%d headers=%v", method, path, w.Code, w.Result().Header)
						}
					}
				}

				w = h.request(http.MethodPost, "/auth/login/login-only", url.Values{"credential": {"wrong"}}, htmx)
				if w.Code != http.StatusUnauthorized || strings.TrimSpace(w.Body.String()) != "Test credential rejected" || w.Header().Get("Location") != "" || w.Header().Get("HX-Redirect") != "" {
					t.Fatalf("provider failure response changed: %d %s", w.Code, w.Body.String())
				}
				h.assertRowCount(t, "session", 0)
				h.assertRowCount(t, "user", 1)
				h.assertRowCount(t, "usertoken", 0)

				w = h.request(http.MethodPost, "/auth/login/login-only", url.Values{"credential": {"test-only-credential"}}, htmx)
				assertRedirectResponse(t, w, "/", htmx)
				h.authenticationCookie(t) // The harness only saves cookies from committed response headers.
				h.assertRowCount(t, "session", 1)
				w = h.request(http.MethodGet, "/", nil, htmx)
				if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), account.Email) {
					t.Fatalf("login session did not restore the account: %d %s", w.Code, w.Body.String())
				}

				// Credential changes require authentication. Check the unsupported
				// workflow after login so the access guard allows it to reach dispatch.
				for _, method := range []string{http.MethodGet, http.MethodPost} {
					w = h.request(method, "/auth/change/login-only", nil, htmx)
					if w.Code != http.StatusNotFound || w.Result().Header.Get("Cache-Control") != "no-store" {
						t.Fatalf("unsupported change: status=%d headers=%v", w.Code, w.Result().Header)
					}
				}
				if len(h.mail.snapshot()) != 0 {
					t.Fatal("login-only workflow attempted mail delivery")
				}
			}, func(cfg *config.Config) {
				// Keep registration and required verification enabled. Neither must
				// require this login-only provider to implement registration or mail.
				cfg.Auth.Services = map[string]map[string]string{"login-only": {"enabled": "true"}}
			})
		})
	}
}
