package blog

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

type basicAccountRepository struct{ user.AccountRepository }

func TestBlogSingleAccountLoginChoices(t *testing.T) {
	for _, kind := range []string{"none", "email", "google", "linked", "multiple", "unverified", "unlinked Google registration", "unsupported storage"} {
		t.Run(kind, func(t *testing.T) {
			cfg := harnessConfig(t, 10)
			cfg.Auth.Services["google"] = map[string]string{"enabled": "true", "clientID": "test-client", "clientSecret": "test-secret"}
			h := startHarness(t, cfg)
			accounts := h.app.Accounts
			identity := user.ExternalIdentity{Issuer: "https://accounts.google.com", Subject: "owner"}
			owner := &user.User{Email: h.blog.access.setup.email, Verified: true, RegistrationAuthType: "email", Password: "hash"}
			if kind == "google" || kind == "unlinked Google registration" {
				owner.Password, owner.RegistrationAuthType = "", "google"
			}
			if kind == "unverified" {
				owner.Verified, owner.VerificationRequired = false, true
			}
			if kind == "google" {
				if err := accounts.(user.ExternalIdentityRepository).CreateExternalAccount(t.Context(), owner, identity); err != nil {
					t.Fatal(err)
				}
			} else if kind != "none" {
				if err := accounts.Create(t.Context(), owner); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "linked" {
				if err := accounts.(user.ExternalIdentityRepository).LinkExternalIdentity(t.Context(), owner, identity); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "multiple" {
				if err := accounts.Create(t.Context(), &user.User{Email: "other@example.com"}); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "unsupported storage" {
				h.blog.access.setup.accounts = basicAccountRepository{accounts}
			}
			for _, hx := range []bool{false, true} {
				w := h.request("GET", "/login", nil, hx)
				if kind == "unlinked Google registration" {
					requireStatus(t, w, 503)
					continue
				}
				requireStatus(t, w, 200)
				wantEmail := kind != "google"
				wantGoogle := kind != "email" && kind != "unverified"
				if strings.Contains(w.Body.String(), `name="password"`) != wantEmail || strings.Contains(w.Body.String(), "Sign in with Google") != wantGoogle {
					t.Fatal("incorrect single-account choices", w.Body)
				}
				// Availability controls presentation, not admission. A submitted
				// email form must preserve the same filtered alternatives on error.
				w = h.request("POST", "/login", url.Values{"provider": {"email"}, "email": {"invalid"}}, hx)
				if strings.Contains(w.Body.String(), "Sign in with Google") != wantGoogle {
					t.Fatal("validation changed login alternatives", w.Body)
				}
				requireStatus(t, h.request("GET", "/auth/reset/request/email", nil, hx), 200)
			}
		})
	}
}

func TestAccountControlsAreSeparateFromSiteProfile(t *testing.T) {
	cfg := harnessConfig(t, 10)
	cfg.Auth.Services["google"] = map[string]string{"enabled": "true", "clientID": "test-client", "clientSecret": "test-secret"}
	h := startHarness(t, cfg)
	h.register(t)
	h.verify(t)
	h.login(t)
	requireStatus(t, h.request("GET", authReturnURL, nil, false), 303)
	for _, hx := range []bool{false, true} {
		w := h.request("GET", "/profile", nil, hx)
		requireStatus(t, w, 200)
		if strings.Contains(w.Body.String(), "/auth/link/google") {
			t.Fatal("site profile still contains account linking")
		}
		if !hx && !strings.Contains(w.Body.String(), `href="/auth/account"`) {
			t.Fatal("account navigation missing")
		}
		w = h.request("GET", "/auth/account", nil, hx)
		requireStatus(t, w, 200)
		if !strings.Contains(w.Body.String(), `href="/auth/link/google"`) || strings.Contains(w.Body.String(), "reader-search-form") {
			t.Fatal("account page lost linking or included reader search")
		}
	}
}

func TestBlogAuthenticationChoices(t *testing.T) {
	for _, mode := range []string{"email", "google", "both"} {
		t.Run(mode, func(t *testing.T) {
			cfg := harnessConfig(t, 10)
			cfg.Auth.Services["google"] = map[string]string{"enabled": "false", "clientID": "test-client", "clientSecret": "test-secret"}
			if mode != "email" {
				cfg.Auth.Services["google"]["enabled"] = "true"
			}
			if mode == "google" {
				cfg.Auth.Services["email"]["enabled"] = "false"
			}
			h := startHarness(t, cfg)
			if w := h.request("GET", "/", nil, false); w.Header().Get("Location") != "/auth/register" {
				t.Fatal("wizard bypasses provider choices", w.Header())
			}
			for _, hx := range []bool{false, true} {
				w := h.request("GET", "/login", nil, hx)
				requireStatus(t, w, 200)
				if strings.Contains(w.Body.String(), `name="password"`) != (mode != "google") || strings.Contains(w.Body.String(), "Sign in with Google") != (mode != "email") {
					t.Fatal("incorrect blog login choices", w.Body)
				}
				w = h.request("GET", "/auth/register", nil, hx)
				requireStatus(t, w, 200)
				if strings.Contains(w.Body.String(), "Sign up with Google") != (mode != "email") {
					t.Fatal("incorrect registration choices", w.Body)
				}
				if strings.Contains(w.Body.String(), `id="register-form"`) != (mode != "google") {
					t.Fatal("registration did not show the email form inline", w.Body)
				}
				if mode == "both" {
					if strings.Contains(w.Body.String(), `href="/auth/register/email"`) ||
						!strings.Contains(w.Body.String(), `action="/auth/register/google"`) {
						t.Fatal("registration still requires an intermediate provider page", w.Body)
					}
					w = h.request("POST", "/auth/register/email", url.Values{"email": {"invalid"}}, hx)
					if !strings.Contains(w.Body.String(), `id="register-form"`) || !strings.Contains(w.Body.String(), `action="/auth/register/google"`) {
						t.Fatal("validation removed registration alternatives", w.Body)
					}
				}
			}
		})
	}
}

func TestAuthenticationPolicyUsesBlogAdmission(t *testing.T) {
	h := newHarness(t)
	policy := h.blog.site.AuthenticationPolicy()
	for _, address := range []string{"trentnix@gmail.com", "stranger@example.com"} {
		allowed, err := policy.AllowAccount(context.Background(), address)
		if err != nil || allowed != (address == "trentnix@gmail.com") {
			t.Fatalf("admission %s: %t %v", address, allowed, err)
		}
	}
}
