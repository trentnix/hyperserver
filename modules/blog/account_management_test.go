package blog

import (
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/auth/identity"
	"github.com/trentnix/hyperserver/auth/password"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func accountHarness(t *testing.T, emailEnabled bool) (*harness, *user.User) {
	t.Helper()
	cfg := harnessConfig(t, 10)
	cfg.Auth.Services["google"] = map[string]string{"enabled": "true", "clientID": "test-client", "clientSecret": "test-secret"}
	if !emailEnabled {
		cfg.Auth.Services["email"]["enabled"] = "false"
	}
	h := startHarness(t, cfg)
	u := &user.User{Email: h.blog.access.setup.email, Verified: true, VerificationRequired: true, RegistrationAuthType: "google"}
	if err := h.app.Accounts.(user.ExternalIdentityRepository).CreateExternalAccount(t.Context(), u, user.ExternalIdentity{Issuer: "https://accounts.google.com", Subject: "owner"}); err != nil {
		t.Fatal(err)
	}
	h.blog.access.setup.complete.Store(true)
	signInAccount(t, h, u)
	return h, u
}

func signInAccount(t *testing.T, h *harness, u *user.User) {
	t.Helper()
	r := session.AddSessionManagerToRequestContext(httptest.NewRequest("GET", "https://blog.example/", nil), h.app.Sessions)
	w := httptest.NewRecorder()
	if err := identity.SetAuthenticatedUser(w, r, u); err != nil {
		t.Fatal(err)
	}
	for _, c := range w.Result().Cookies() {
		h.cookies[c.Name] = c
	}
}

func TestAccountInlineForms(t *testing.T) {
	for _, action := range []string{"setup", "disconnect"} {
		t.Run(action, func(t *testing.T) {
			h, u := accountHarness(t, true)
			path := "/auth/password/setup/email"
			if action == "disconnect" {
				var err error
				u.Password, err = password.HashPassword("Working-Password-123!")
				if err != nil {
					t.Fatal(err)
				}
				if err := h.app.Accounts.Update(t.Context(), u); err != nil {
					t.Fatal(err)
				}
				signInAccount(t, h, u)
				path = "/auth/unlink/google"
			}
			for _, target := range []string{"article-content", "account-action-0-0"} {
				for _, method := range []string{"GET", "POST"} {
					if action == "setup" && method == "POST" {
						h.mail.err = errors.New("delivery unavailable")
					}
					r := httptest.NewRequest(method, "https://blog.example"+path, strings.NewReader("password=wrong"))
					r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					r.Header.Set("HX-Request", "true")
					r.Header.Set("HX-Target", "div#"+target)
					for _, cookie := range h.cookies {
						r.AddCookie(cookie)
					}
					w := httptest.NewRecorder()
					h.app.Handler.ServeHTTP(w, r)
					want := http.StatusOK
					if action == "setup" && method == "POST" {
						want = http.StatusServiceUnavailable
					}
					requireStatus(t, w, want)
					body := w.Body.String()
					if strings.Contains(body, "<h1") != (target == "article-content") || !strings.Contains(body, `class="account-action-panel"`) {
						t.Fatal("incorrect full-page or inline form", body)
					}
					if method == "POST" && !strings.Contains(body, `class="form-error"`) {
						t.Fatal("inline failure lost its error message", body)
					}
				}
			}
		})
	}
}

func TestUnusedSetupLinkCannotResetAfterEnrollment(t *testing.T) {
	h, u := accountHarness(t, true)
	links := make([]*url.URL, 2)
	for i := range links {
		requireStatus(t, h.request("POST", "/auth/password/setup/email", url.Values{}, false), 200)
		match := regexp.MustCompile(`href="([^"]+)"`).FindStringSubmatch(h.mail.messages[i].Body)
		var err error
		links[i], err = url.Parse(html.UnescapeString(match[1]))
		if err != nil {
			t.Fatal(err)
		}
	}
	first := "Initial-Password-123!"
	requireStatus(t, h.request("POST", links[0].RequestURI(), url.Values{"password": {first}, "passwordMatch": {first}}, false), 303)
	h.cookies = make(map[string]*http.Cookie)
	second := "Unexpected-Password-123!"
	w := h.request("POST", "/auth/reset/email?"+links[1].RawQuery, url.Values{"password": {second}, "passwordMatch": {second}}, false)
	requireStatus(t, w, 400)
	stored, err := h.app.Accounts.GetByID(t.Context(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !password.CheckPasswordHash(first, stored.Password) {
		t.Fatalf("unused enrollment link changed password through anonymous reset endpoint: HTTP %d", w.Code)
	}
}

func TestSharedDisconnectionSupportsAnotherProvider(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "no password provider", true: "password provider"}[enabled], func(t *testing.T) {
			cfg := harnessConfig(t, 10)
			cfg.Auth.Services["email"]["enabled"] = map[bool]string{false: "false", true: "true"}[enabled]
			cfg.Auth.Services["test-external"] = map[string]string{"enabled": "true"}
			h := startHarness(t, cfg)
			const supplied = "Working-Password-123!"
			hash, err := password.HashPassword(supplied)
			if err != nil {
				t.Fatal(err)
			}
			u := &user.User{Email: h.blog.access.setup.email, Verified: true, Password: hash}
			repo := h.app.Accounts.(user.ExternalIdentityRepository)
			if err := repo.CreateExternalAccount(t.Context(), u, user.ExternalIdentity{Issuer: "https://identity.example", Subject: "owner"}); err != nil {
				t.Fatal(err)
			}
			h.blog.access.setup.complete.Store(true)
			signInAccount(t, h, u)
			w := h.request("GET", "/auth/account", nil, false)
			requireStatus(t, w, 200)
			if strings.Contains(w.Body.String(), "Disconnect Example") != enabled {
				t.Fatal("wrong disconnect availability")
			}
			w = h.request("POST", "/auth/unlink/test-external", url.Values{"password": {supplied}}, false)
			want := http.StatusForbidden
			if enabled {
				want = http.StatusSeeOther
			}
			requireStatus(t, w, want)
			linked, err := repo.HasExternalIdentity(t.Context(), u.ID, "https://identity.example")
			if err != nil || linked == enabled {
				t.Fatal("incorrect disconnection", err)
			}
		})
	}
}

func TestAccountNavigationAndMethods(t *testing.T) {
	for _, emailEnabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "Google only", true: "both enabled"}[emailEnabled], func(t *testing.T) {
			h, _ := accountHarness(t, emailEnabled)
			body := h.request("GET", "/post/manage", nil, false).Body.String()
			header := regexp.MustCompile(`(?s)<header class="site-header".*?</header>`).FindString(body)
			bar := regexp.MustCompile(`(?s)<nav class="account-nav".*?</nav>`).FindString(body)
			if strings.Contains(bar, "/auth/account") || strings.Contains(bar, "/auth/logout") {
				t.Fatal("personal controls remain in content navigation")
			}
			if strings.Contains(header, `href="/auth/account"`) != emailEnabled || strings.Contains(header, `action="/auth/logout"`) == emailEnabled {
				t.Fatal("incorrect personal navigation", header)
			}
			if strings.Contains(header, `href="/login"`) {
				t.Fatal("signed-in header offers login")
			}
			for _, hx := range []bool{false, true} {
				w := h.request("GET", "/auth/account", nil, hx)
				requireStatus(t, w, 200)
				body = w.Body.String()
				if !strings.Contains(body, "Connected") || strings.Contains(body, "Add password") != emailEnabled || strings.Contains(body, "Disconnect Google") {
					t.Fatal("incorrect account methods", body)
				}
				if strings.Contains(body, "reader-search-form") {
					t.Fatal("account contains reader search")
				}
				requireStatus(t, h.request("POST", "/auth/unlink/google", url.Values{"password": {"guess"}}, hx), 403)
			}
			if !emailEnabled {
				requireStatus(t, h.request("GET", "/auth/password/setup/email", nil, false), 404)
			}
		})
	}
}

func TestAccountPasswordEnrollmentAndGoogleDisconnect(t *testing.T) {
	for _, hx := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "HTMX"}[hx], func(t *testing.T) {
			h, u := accountHarness(t, true)
			oldCookie := h.cookies["auth-user-session"]
			requireStatus(t, h.request("GET", "/auth/password/setup/email", nil, hx), 200)
			if len(h.mail.messages) != 0 {
				t.Fatal("GET sent mail")
			}
			w := h.request("POST", "/auth/password/setup/email", url.Values{"email": {"attacker@example.com"}}, hx)
			requireStatus(t, w, 200)
			if !strings.Contains(w.Body.String(), "Check your email") || len(h.mail.messages) != 1 {
				t.Fatal("missing setup confirmation", w.Body)
			}
			mail := h.mail.messages[0]
			if mail.To != u.Email || mail.Subject != "Set up your password" {
				t.Fatal("incorrect setup recipient or subject")
			}
			match := regexp.MustCompile(`href="([^"]+)"`).FindStringSubmatch(mail.Body)
			if len(match) != 2 {
				t.Fatal("setup link missing")
			}
			link, err := url.Parse(html.UnescapeString(match[1]))
			if err != nil {
				t.Fatal(err)
			}
			token := link.Query().Get("token")
			if strings.Contains(w.Body.String(), token) {
				t.Fatal("setup response exposed token")
			}
			w = h.request("GET", link.RequestURI(), nil, hx)
			requireStatus(t, w, 200)
			if !strings.Contains(w.Body.String(), "Create a password") {
				t.Fatal("wrong setup form")
			}
			w = h.request("POST", link.RequestURI(), url.Values{"password": {"short"}, "passwordMatch": {"short"}}, hx)
			requireStatus(t, w, 200)
			if !strings.Contains(w.Body.String(), `aria-invalid="true"`) {
				t.Fatal("missing inline validation", w.Body)
			}
			const newPassword = "New-Password-123!"
			w = h.request("POST", link.RequestURI(), url.Values{"password": {newPassword}, "passwordMatch": {newPassword}}, hx)
			if w.Header().Get("Location") != "/login" && w.Header().Get("HX-Redirect") != "/login" {
				t.Fatal("setup did not return to login", w.Header(), w.Body)
			}
			stored, err := h.app.Accounts.GetByID(t.Context(), u.ID)
			if err != nil || !password.CheckPasswordHash(newPassword, stored.Password) || stored.RegistrationAuthType != "google" || stored.SessionVersion != u.SessionVersion+1 {
				t.Fatal("incorrect enrollment", err)
			}
			if _, err := user.ValidatePasswordSetupToken(t.Context(), h.app.Accounts, token); !errors.Is(err, user.ErrTokenNotFound) {
				t.Fatal("setup token replayable", err)
			}
			h.cookies["auth-user-session"] = oldCookie
			w = h.request("GET", "/auth/account", nil, false)
			requireStatus(t, w, 303)
			delete(h.cookies, "auth-user-session")
			w = h.request("GET", "/login", nil, false)
			if !strings.Contains(w.Body.String(), `name="password"`) || !strings.Contains(w.Body.String(), "Sign in with Google") {
				t.Fatal("enrolled methods not offered", w.Body)
			}
			if !strings.Contains(w.Body.String(), "You can now sign in with your email and password. Your other sign-in options are unchanged.") {
				t.Fatal("missing password creation confirmation", w.Body)
			}
			requireStatus(t, h.request("POST", "/login", url.Values{"provider": {"email"}, "email": {u.Email}, "password": {newPassword}}, false), 303)
			w = h.request("GET", "/auth/account", nil, hx)
			requireStatus(t, w, 200)
			if !strings.Contains(w.Body.String(), "Disconnect Google") || !strings.Contains(w.Body.String(), "Change password") {
				t.Fatal("missing enrolled account controls", w.Body)
			}
			w = h.request("POST", "/auth/unlink/google", url.Values{"password": {"wrong"}}, hx)
			requireStatus(t, w, 200)
			if !strings.Contains(w.Body.String(), `aria-invalid="true"`) || !strings.Contains(w.Body.String(), "That password is incorrect") || strings.Contains(w.Body.String(), `value="wrong"`) {
				t.Fatal("wrong password did not give safe inline error", w.Body)
			}
			connected, err := h.app.Accounts.(user.ExternalIdentityRepository).HasExternalIdentity(t.Context(), u.ID, "https://accounts.google.com")
			if err != nil || !connected {
				t.Fatal("wrong password disconnected Google", err)
			}
			w = h.request("POST", "/auth/unlink/google", url.Values{"password": {newPassword}}, hx)
			if w.Header().Get("Location") != "/login" && w.Header().Get("HX-Redirect") != "/login" {
				t.Fatal("disconnect did not return to login", w.Header(), w.Body)
			}
			connected, err = h.app.Accounts.(user.ExternalIdentityRepository).HasExternalIdentity(t.Context(), u.ID, "https://accounts.google.com")
			if err != nil || connected {
				t.Fatal("Google remains connected", err)
			}
			w = h.request("GET", "/login", nil, false)
			if !strings.Contains(w.Body.String(), `name="password"`) || strings.Contains(w.Body.String(), "Sign in with Google") {
				t.Fatal("login did not reflect disconnected method", w.Body)
			}
			before := len(h.mail.messages)
			requireStatus(t, h.request("POST", "/auth/reset/request/email", url.Values{"email": {u.Email}}, hx), 200)
			if len(h.mail.messages) != before+1 {
				t.Fatal("Google-registered account cannot recover its password")
			}
		})
	}
}

func TestPasswordEnrollmentFailures(t *testing.T) {
	for _, scenario := range []string{"signed out", "unverified", "wrong account", "expired", "changed email", "mail failure", "public recovery bypass"} {
		t.Run(scenario, func(t *testing.T) {
			h, u := accountHarness(t, true)
			token, err := user.NewPasswordSetupToken(u, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			token.Email = u.Email
			if scenario == "expired" {
				token.ExpiresAt = time.Now().Add(-time.Hour)
			}
			if scenario == "changed email" {
				token.Email = "previous@example.com"
			}
			if err := h.app.Accounts.InsertToken(t.Context(), token.Metadata()); err != nil {
				t.Fatal(err)
			}
			if scenario == "signed out" {
				h.cookies = make(map[string]*http.Cookie)
			}
			if scenario == "unverified" {
				u.Verified = false
				if err := h.app.Accounts.Update(t.Context(), u); err != nil {
					t.Fatal(err)
				}
				signInAccount(t, h, u)
			}
			if scenario == "wrong account" {
				other := &user.User{Email: "other@example.com", Verified: true}
				if err := h.app.Accounts.Create(t.Context(), other); err != nil {
					t.Fatal(err)
				}
				token, err = user.NewPasswordSetupToken(other, time.Hour)
				if err != nil {
					t.Fatal(err)
				}
				if err := h.app.Accounts.InsertToken(t.Context(), token.Metadata()); err != nil {
					t.Fatal(err)
				}
			}
			path := "/auth/password/setup/email/complete?" + url.Values{"token": {token.Token}}.Encode()
			want := 400
			if scenario == "signed out" {
				want = 303
			}
			if scenario == "unverified" {
				want = 403
			}
			if scenario == "public recovery bypass" {
				h.cookies = make(map[string]*http.Cookie)
				path = "/auth/reset/email?" + url.Values{"token": {token.Token}}.Encode()
				want = 400
			}
			if scenario == "mail failure" {
				h.mail.err = errors.New("private delivery error with token")
				path = "/auth/password/setup/email"
				want = 503
			}
			w := h.request("POST", path, url.Values{"password": {"New-Password-123!"}, "passwordMatch": {"New-Password-123!"}}, false)
			requireStatus(t, w, want)
			if strings.Contains(w.Body.String(), "private delivery") {
				t.Fatal("delivery details leaked")
			}
			stored, err := h.app.Accounts.GetByID(t.Context(), u.ID)
			if err != nil || stored.Password != "" {
				t.Fatal("failed enrollment changed password", err)
			}
		})
	}
}

func TestGoogleDisconnectRequiresEnabledPasswordLogin(t *testing.T) {
	h, u := accountHarness(t, false)
	hash, err := password.HashPassword("Working-Password-123!")
	if err != nil {
		t.Fatal(err)
	}
	u.Password = hash
	if err := h.app.Accounts.Update(t.Context(), u); err != nil {
		t.Fatal(err)
	}
	signInAccount(t, h, u)
	for _, hx := range []bool{false, true} {
		requireStatus(t, h.request("POST", "/auth/unlink/google", url.Values{"password": {"Working-Password-123!"}}, hx), http.StatusForbidden)
	}
	connected, err := h.app.Accounts.(user.ExternalIdentityRepository).HasExternalIdentity(t.Context(), u.ID, "https://accounts.google.com")
	if err != nil || !connected {
		t.Fatal("disabled password login allowed disconnection", err)
	}
}

func TestPasswordEnrollmentCannotUseChangeRoute(t *testing.T) {
	h, u := accountHarness(t, true)
	w := h.request("POST", "/auth/change/email", url.Values{"oldPassword": {"anything"}, "newPassword": {"New-Password-123!"}, "newPasswordMatch": {"New-Password-123!"}}, false)
	if !strings.Contains(w.Body.String(), "incorrect") {
		t.Fatal("change route accepted enrollment", w.Body)
	}
	stored, err := h.app.Accounts.GetByID(t.Context(), u.ID)
	if err != nil || stored.Password != "" {
		t.Fatal("change route bypassed emailed enrollment", err)
	}
}
