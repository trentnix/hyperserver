package blog

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

// TestSetupVerificationGuidance keeps login available without competing with verification.
func TestSetupVerificationGuidance(t *testing.T) {
	h := startHarness(t, harnessConfig(t, 10))
	h.register(t)
	w := h.request("GET", loginURL, nil, false)
	requireStatus(t, w, http.StatusOK)
	for _, want := range []string{"<h1>Check your email</h1>", `class="setup-login">`, "sign in to request another", `id="login-form"`} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, hx := range []bool{false, true} {
		w = h.request("POST", "/login", url.Values{"provider": {"email"}, "email": {"trentnix@gmail.com"}, "password": {"wrong"}}, hx)
		requireStatus(t, w, http.StatusOK)
		if !strings.Contains(w.Body.String(), "Invalid login/password") || !hx && !strings.Contains(w.Body.String(), `class="setup-login" open>`) {
			t.Fatal("failed login must leave validation visible")
		}
	}
	// Verification in a different browser must not depend on registration cookies.
	originalBrowser := h.cookies
	h.cookies = make(map[string]*http.Cookie)
	h.verify(t)
	h.cookies = originalBrowser
	w = h.request("GET", loginURL, nil, false)
	requireStatus(t, w, http.StatusOK)
	if strings.Contains(w.Body.String(), "setup-login") || !strings.Contains(w.Body.String(), "Sign in to your blog") {
		t.Fatal("verified account must see login directly in the original browser")
	}
	requireStatus(t, h.request("GET", "/", nil, false), http.StatusOK)
}

// TestSetupExpiredVerification provides a recovery path without opening the site.
func TestSetupExpiredVerification(t *testing.T) {
	h := startHarness(t, harnessConfig(t, 10))
	h.register(t)
	account, err := h.app.Accounts.GetByEmail(t.Context(), "trentnix@gmail.com")
	if err != nil {
		t.Fatal(err)
	}
	token, err := user.NewAuthVerificationToken(account, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	metadata := token.Metadata()
	metadata.ExpiresAt = time.Now().Add(-time.Hour)
	if err := h.app.Accounts.InsertToken(t.Context(), metadata); err != nil {
		t.Fatal(err)
	}
	for _, hx := range []bool{false, true} {
		w := h.request("POST", "/auth/verify", url.Values{"token": {token.Token}}, hx)
		requireStatus(t, w, http.StatusBadRequest)
		if !strings.Contains(w.Body.String(), "invalid or expired") || !strings.Contains(w.Body.String(), `href="/auth/continue"`) {
			t.Fatal("expired verification needs an explanation and a way to continue")
		}
	}
	requireStatus(t, h.request("GET", "/", nil, false), http.StatusSeeOther)
	h.login(t)
	w := h.request("GET", authReturnURL, nil, false)
	if w.Header().Get("Location") != "/auth/request/verify" {
		t.Fatal("unverified login must lead to resend instructions")
	}
	// Retain only the replacement message so verify exercises the new link.
	h.mail.messages = nil
	requireStatus(t, h.request("POST", "/auth/request/verify", nil, false), http.StatusOK)
	h.verify(t)
	w = h.request("GET", authReturnURL, nil, false)
	if w.Header().Get("Location") != "/profile" {
		t.Fatal("replacement verification must finish setup for the signed-in administrator")
	}
}

// TestSetupProfileGuidance survives leaving the editor and finishes only after a successful save.
func TestSetupProfileGuidance(t *testing.T) {
	for _, hx := range []bool{false, true} {
		t.Run(fmt.Sprint(hx), func(t *testing.T) {
			cfg := harnessConfig(t, 10)
			h := startHarness(t, cfg)
			h.register(t)
			h.verify(t)
			h.login(t)
			requireStatus(t, h.request("GET", authReturnURL, nil, false), http.StatusSeeOther)
			const introduction = "Give your blog a name and a short introduction. Images are optional."
			for range 2 {
				w := h.request("GET", "/profile?created=1", nil, hx)
				requireStatus(t, w, http.StatusOK)
				if !strings.Contains(w.Body.String(), introduction) || strings.Contains(w.Body.String(), "setup-complete") {
					t.Fatal("unsaved profile needs guidance, not a success message")
				}
				if err := h.app.ShutdownContext(t.Context()); err != nil {
					t.Fatal(err)
				}
				h = startHarness(t, cfg)
				h.login(t)
			}
			fields := url.Values{"version": {"0"}, "name": {""}}
			requireStatus(t, h.request("POST", "/profile", fields, hx), http.StatusUnprocessableEntity)
			fields.Set("name", "My new blog")
			store := h.blog.profileStore
			h.blog.profileStore = failingProfileStore{store}
			w := h.request("POST", "/profile", fields, hx)
			requireStatus(t, w, http.StatusInternalServerError)
			if strings.Contains(w.Body.String(), "setup-complete") || h.blog.profile.Current().Version != 0 || w.Header().Get("HX-Redirect") != "" || w.Header().Get("Location") != "" {
				t.Fatal("failed save must not finish profile setup")
			}
			h.blog.profileStore = store
			w = h.request("POST", "/profile", fields, hx)
			status, header := http.StatusSeeOther, "Location"
			if hx {
				status, header = http.StatusOK, "HX-Redirect"
			}
			requireStatus(t, w, status)
			if w.Header().Get(header) != "/profile?created=1" {
				t.Fatal("first save must lead to confirmation")
			}
			w = h.request("GET", "/profile?created=1", nil, hx)
			requireStatus(t, w, http.StatusOK)
			for _, want := range []string{"Your site profile is saved.", `href="/">View site`, `href="/post/create">Write your first post`} {
				if !strings.Contains(w.Body.String(), want) {
					t.Errorf("missing %q", want)
				}
			}
			if strings.Contains(w.Body.String(), introduction) {
				t.Fatal("saved profile still shows initial guidance")
			}
			requireStatus(t, h.request("POST", "/profile", fields, hx), http.StatusConflict)
			if strings.Contains(h.request("GET", "/profile", nil, hx).Body.String(), "setup-complete") {
				t.Fatal("returning to the editor must not replay confirmation")
			}
			fields.Set("version", "1")
			fields.Set("name", "Updated blog")
			w = h.request("POST", "/profile", fields, hx)
			requireStatus(t, w, status)
			if w.Header().Get(header) != "/profile" || strings.Contains(h.request("GET", "/profile?created=1", nil, hx).Body.String(), "setup-complete") {
				t.Fatal("later saves must not replay first-save confirmation")
			}
		})
	}
}

// TestSetupAuthenticationReturns keeps verification and login guidance separate
// from public navigation, for ordinary forms and HTMX submissions.
func TestSetupAuthenticationReturns(t *testing.T) {
	for _, hx := range []bool{false, true} {
		t.Run(fmt.Sprint(hx), func(t *testing.T) {
			h := startHarness(t, harnessConfig(t, 10))
			h.register(t)
			assertRedirect := func(w *httptest.ResponseRecorder, target string) {
				t.Helper()
				header, status := "Location", http.StatusSeeOther
				if hx {
					header, status = "HX-Redirect", http.StatusOK
				}
				requireStatus(t, w, status)
				if w.Header().Get(header) != target {
					t.Fatalf("redirect=%q, want %q", w.Header().Get(header), target)
				}
			}
			link := regexp.MustCompile(`https://blog.example/auth/verify\?[^"< ]+`).FindString(h.mail.messages[0].Body)
			u, err := url.Parse(html.UnescapeString(link))
			if err != nil || u.Query().Get("token") == "" {
				t.Fatalf("missing verification link: %v", err)
			}
			assertRedirect(h.request("POST", "/auth/verify", url.Values{"token": {u.Query().Get("token")}}, hx), authReturnURL)
			assertRedirect(h.request("GET", authReturnURL, nil, hx), loginURL)
			requireStatus(t, h.request("GET", "/", nil, hx), http.StatusOK)
			assertRedirect(h.request("POST", "/login", credentials("trentnix@gmail.com"), hx), authReturnURL)
			assertRedirect(h.request("GET", authReturnURL, nil, hx), "/profile")
			requireStatus(t, h.request("GET", "/profile", nil, hx), http.StatusOK)
			assertRedirect(h.request("POST", "/auth/logout", nil, hx), authReturnURL)
			w := h.request("GET", authReturnURL, nil, hx)
			assertRedirect(w, "/")
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("authentication return must not be cached")
			}
			requireStatus(t, h.request("GET", "/", nil, hx), http.StatusOK)
		})
	}
}

// TestSetupRegistrationNavigation hides login until an administrator account exists,
// including validation responses and browsers with a returning-user cookie.
func TestSetupRegistrationNavigation(t *testing.T) {
	h := startHarness(t, harnessConfig(t, 10))
	h.cookies[returningUserCookie] = &http.Cookie{Name: returningUserCookie, Value: "1"}
	for _, hx := range []bool{false, true} {
		for _, method := range []string{"GET", "POST"} {
			w := h.request(method, "/register", url.Values{"provider": {"email"}, "email": {"trentnix@gmail.com"}}, hx)
			requireStatus(t, w, http.StatusOK)
			body := w.Body.String()
			if !strings.Contains(body, `id="register-form"`) || strings.Contains(body, `href="/login"`) {
				t.Fatalf("%s HTMX=%v: registration must render without a login link", method, hx)
			}
			if method == "POST" && !strings.Contains(body, "This field is required.") {
				t.Fatal("expected registration validation errors")
			}
		}
	}
}

// TestSetupLifecycle covers first registration, verification, first login, and restarts.
func TestSetupLifecycle(t *testing.T) {
	cfg := harnessConfig(t, 10)
	h := startHarness(t, cfg)
	redirect := func(h *harness, path, target string) {
		t.Helper()
		for _, hx := range []bool{false, true} {
			w := h.request("GET", path, nil, hx)
			if hx {
				requireStatus(t, w, http.StatusOK)
				if w.Header().Get("HX-Redirect") != target {
					t.Fatalf("%s: wrong HTMX redirect: %v", path, w.Header())
				}
			} else {
				requireStatus(t, w, http.StatusSeeOther)
				if w.Header().Get("Location") != target {
					t.Fatalf("%s: wrong redirect: %v", path, w.Header())
				}
			}
		}
	}
	for _, path := range []string{"/", "/posts", "/search?q=test", "/posts/recent", "/rss", "/sitemap.xml", "/robots.txt", "/page/about", "/post/s/test", "/images/test", "/profile", "/pages", "/missing"} {
		redirect(h, path, "/register")
	}
	w := h.request("GET", "/register", nil, false)
	requireStatus(t, w, http.StatusOK)
	for _, want := range []string{"Set up your blog", `id="register-form"`, `class="setup-panel"`} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, forbidden := range []string{"trentnix", "site-sidebar", "Search Site"} {
		if strings.Contains(w.Body.String(), forbidden) {
			t.Errorf("setup exposes %q", forbidden)
		}
	}
	requireStatus(t, h.request("GET", "/assets/css/blog.css", nil, false), http.StatusOK)
	requireStatus(t, h.request("POST", "/register", credentials("other@example.test"), false), http.StatusForbidden)
	h.register(t)
	redirect(h, "/", loginURL)
	redirect(h, "/posts", loginURL)
	redirect(h, "/register", loginURL)
	if !strings.Contains(h.request("GET", loginURL, nil, false).Body.String(), "Check your email") {
		t.Fatal("missing verification guidance")
	}
	if err := h.app.ShutdownContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	mail := h.mail
	h = startHarness(t, cfg)
	h.mail = mail // Retain the verification message sent before the restart.
	redirect(h, "/posts", loginURL)
	h.verify(t)
	// Verification opens public content before the first login.
	requireStatus(t, h.request("GET", "/posts", nil, false), http.StatusOK)
	for _, hx := range []bool{false, true} {
		requireStatus(t, h.request("GET", "/", nil, hx), http.StatusOK)
	}
	redirect(h, authReturnURL, loginURL)
	if err := h.app.ShutdownContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	h = startHarness(t, cfg)
	requireStatus(t, h.request("GET", "/", nil, false), http.StatusOK)
	redirect(h, authReturnURL, loginURL)
	if h.blog.access.setup.complete.Load() {
		t.Fatal("restart skipped the first login")
	}
	requireStatus(t, h.request("POST", "/login", url.Values{"provider": {"email"}, "email": {"trentnix@gmail.com"}, "password": {"wrong"}}, false), http.StatusOK)
	if h.blog.access.setup.complete.Load() {
		t.Fatal("failed login completed setup")
	}
	h.login(t)
	w = h.request("GET", authReturnURL, nil, false)
	requireStatus(t, w, http.StatusSeeOther)
	if w.Header().Get("Location") != "/profile" {
		t.Fatal("first login did not lead to Site Profile")
	}
	requireStatus(t, h.request("GET", "/profile", nil, false), http.StatusOK)
	if !h.blog.access.setup.complete.Load() {
		t.Fatal("first login did not complete setup")
	}
	if err := h.app.ShutdownContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	h = startHarness(t, cfg)
	requireStatus(t, h.request("GET", "/", nil, false), http.StatusOK)
	redirect(h, authReturnURL, "/")
	if strings.Contains(h.request("GET", loginURL, nil, false).Body.String(), "setup-panel") {
		t.Fatal("completed setup still shows wizard")
	}
}

// TestSetupWithoutVerification respects the configured registration policy.
func TestSetupWithoutVerification(t *testing.T) {
	cfg := harnessConfig(t, 10)
	cfg.Auth.RegisterRequiresVerification = false
	h := startHarness(t, cfg)
	requireStatus(t, h.request("POST", "/register", credentials("trentnix@gmail.com"), false), http.StatusSeeOther)
	if len(h.mail.messages) != 0 {
		t.Fatal("sent verification despite disabled policy")
	}
	requireStatus(t, h.request("GET", "/posts", nil, false), http.StatusOK)
	requireStatus(t, h.request("GET", "/", nil, false), http.StatusOK)
	h.login(t)
	w := h.request("GET", authReturnURL, nil, false)
	if w.Header().Get("Location") != "/profile" {
		t.Fatalf("first login redirect: %v", w.Header())
	}
	requireStatus(t, h.request("GET", "/profile", nil, false), http.StatusOK)
}

// TestSetupFailures leaves onboarding pending when required operations fail.
func TestSetupFailures(t *testing.T) {
	t.Run("registration disabled", func(t *testing.T) {
		cfg := harnessConfig(t, 10)
		cfg.Auth.RegistrationEnabled = false
		h := startHarness(t, cfg)
		w := h.request("GET", "/", nil, false)
		requireStatus(t, w, http.StatusServiceUnavailable)
		if !strings.Contains(w.Body.String(), "auth.registrationEnabled") {
			t.Fatal("missing configuration guidance")
		}
	})
	t.Run("mail delivery", func(t *testing.T) {
		h := startHarness(t, harnessConfig(t, 10))
		h.mail.err = errors.New("mail offline")
		h.request("POST", "/register", credentials("trentnix@gmail.com"), false)
		account, err := h.app.Accounts.GetByEmail(t.Context(), "trentnix@gmail.com")
		if err != nil || !account.NeedsVerification() {
			t.Fatalf("expected pending account: %v", err)
		}
		requireStatus(t, h.request("GET", "/posts", nil, false), http.StatusSeeOther)
		h.login(t)
		requireStatus(t, h.request("GET", "/auth/request/verify", nil, false), http.StatusOK)
		if h.blog.access.setup.complete.Load() {
			t.Fatal("unverified login completed setup")
		}
		h.mail.err = nil
		requireStatus(t, h.request("POST", "/auth/request/verify", nil, false), http.StatusOK)
		if len(h.mail.messages) != 1 {
			t.Fatal("could not resend verification")
		}
		h.verify(t)
		if w := h.request("GET", authReturnURL, nil, false); w.Header().Get("Location") != "/profile" {
			t.Fatal("recovered delivery did not allow setup to finish")
		}
	})
	t.Run("account lookup", func(t *testing.T) {
		h := startHarness(t, harnessConfig(t, 10))
		if err := h.blog.persistProfile(t.Context(), Profile{Name: "Private identity"}); err != nil {
			t.Fatal(err)
		}
		h.blog.access.setup.accounts = unavailableAccounts{}
		w := h.request("GET", "/posts", nil, false)
		requireStatus(t, w, http.StatusServiceUnavailable)
		if strings.Contains(w.Body.String(), "Private identity") || strings.Contains(w.Body.String(), "site-sidebar") {
			t.Fatal("setup error exposed site content")
		}
		requireStatus(t, h.request("GET", "/register", nil, false), http.StatusServiceUnavailable)
	})
	t.Run("missing setup record", func(t *testing.T) {
		h := startHarness(t, harnessConfig(t, 10))
		h.register(t)
		h.verify(t)
		h.login(t)
		if _, err := h.blog.posts.(*sqliteRepository).db.ExecContext(t.Context(), "DELETE FROM blog_setup"); err != nil {
			t.Fatal(err)
		}
		requireStatus(t, h.request("GET", "/", nil, false), http.StatusServiceUnavailable)
		if h.blog.access.setup.complete.Load() {
			t.Fatal("missing setup record silently completed setup")
		}
	})
	t.Run("completion storage", func(t *testing.T) {
		h := startHarness(t, harnessConfig(t, 10))
		h.register(t)
		h.verify(t)
		h.login(t)
		if err := h.blog.posts.(*sqliteRepository).db.Close(); err != nil {
			t.Fatal(err)
		}
		requireStatus(t, h.request("GET", "/", nil, false), http.StatusServiceUnavailable)
		if h.blog.access.setup.complete.Load() {
			t.Fatal("failed save completed setup in memory")
		}
	})
}

// TestSetupInitializationFailure does not publish partially initialized module state.
func TestSetupInitializationFailure(t *testing.T) {
	h := newHarness(t)
	settings := *h.blog.settings
	settings.AdminEmail = "another@example.test"
	m := &module{settings: &settings, access: &Access{}, accounts: unavailableAdminAccounts{unavailableAccounts{}, h.blog.administration, h.blog.accounts.(user.InvitationRepository)}}
	if err := m.Init(t.Context(), h.blog.app); err == nil || !strings.Contains(err.Error(), "load blog setup") {
		t.Fatalf("failed account lookup did not abort setup: %v", err)
	}
	if m.access.setup != nil || m.access.emailKey != "" || m.close != nil {
		t.Fatal("failed setup published module state")
	}
	if _, err := h.blog.pageStore.ListPages(t.Context(), true); err != nil {
		t.Fatal("failed setup closed another instance's storage", err)
	}
}

type unavailableAdminAccounts struct {
	unavailableAccounts
	user.AdministrationRepository
	user.InvitationRepository
}

// TestSetupUpgrade preserves established installations without a setup record.
func TestSetupUpgrade(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	if _, err := h.blog.posts.(*sqliteRepository).db.ExecContext(t.Context(), "DELETE FROM blog_setup"); err != nil {
		t.Fatal(err)
	}
	cfg := *h.app.Config
	if err := h.app.ShutdownContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	h = startHarness(t, cfg)
	if !h.blog.access.setup.complete.Load() {
		t.Fatal("upgrade restarted setup")
	}
	requireStatus(t, h.request("GET", "/", nil, false), http.StatusOK)
}

// TestSetupConcurrentCompletion exercises simultaneous requests after the first login.
func TestSetupConcurrentCompletion(t *testing.T) {
	h := startHarness(t, harnessConfig(t, 10))
	h.register(t)
	h.verify(t)
	h.login(t)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			r := httptest.NewRequest("GET", "https://blog.example/", nil)
			for _, c := range h.cookies {
				r.AddCookie(c)
			}
			w := httptest.NewRecorder()
			h.app.Handler.ServeHTTP(w, r)
			if w.Code != http.StatusOK && w.Code != http.StatusSeeOther {
				t.Errorf("concurrent setup: %d", w.Code)
			}
		})
	}
	wg.Wait()
	complete, found, err := h.blog.access.setup.store.setupStatus(t.Context(), user.EmailKey("trentnix@gmail.com"))
	if err != nil || !complete || !found {
		t.Fatalf("completion not saved: %v %v %v", complete, found, err)
	}
}
