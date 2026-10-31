package blog

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"hash/crc32"
	"html"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/application"
	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/messages"
	"github.com/trentnix/hyperserver/pkg/handlers"
	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

type mailbox struct {
	mu       sync.Mutex
	messages []messaging.MailMessage
	err      error
}

// TestReturningUserNavigation keeps account controls out of a new visitor's menu.
func TestReturningUserNavigation(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	requireStatus(t, h.request(http.MethodPost, "/post", url.Values{
		"slug": {"welcome"}, "title": {"Welcome"}, "content": {"Hello"},
	}, false), http.StatusSeeOther)
	h.publish(t, "welcome")

	const link = `href="/auth/login/email">Log in</a>`
	if body := h.request(http.MethodGet, "/post/s/welcome", nil, false).Body.String(); strings.Contains(body, link) || !strings.Contains(body, ">Log out</button>") {
		t.Fatal("signed-in navigation must show logout instead of login")
	}
	requireStatus(t, h.request(http.MethodPost, "/auth/logout", nil, false), http.StatusSeeOther)
	if h.cookies[returningUserCookie] == nil {
		t.Fatal("logout removed the returning-user cookie")
	}

	for _, value := range []string{"1", "", "invalid"} {
		h.cookies = make(map[string]*http.Cookie)
		if value != "" {
			h.cookies[returningUserCookie] = &http.Cookie{Name: returningUserCookie, Value: value}
		}
		for _, path := range []string{"/", "/about", "/post/s/welcome", loginURL} {
			w := h.request(http.MethodGet, path, nil, false)
			requireStatus(t, w, http.StatusOK)
			if got := strings.Contains(w.Body.String(), link); got != (value == "1" && path != loginURL) {
				t.Errorf("cookie=%q path=%s: login link visible=%v", value, path, got)
			}
			if path == loginURL && !strings.Contains(w.Body.String(), `name="password"`) {
				t.Error("direct login form is unavailable")
			}
			for _, cookie := range w.Result().Cookies() {
				if cookie.Name == returningUserCookie {
					t.Error("anonymous reading must not issue a returning-user cookie")
				}
			}
		}
		// Even a forged presentation cookie cannot authorize publishing.
		requireStatus(t, h.request(http.MethodGet, "/post/create", nil, false), http.StatusForbidden)
	}
}

// TestAuthFormNavigation keeps login below registration, including validation responses.
func TestAuthFormNavigation(t *testing.T) {
	h := newHarness(t)
	const link = `href="/auth/login/email">Log in</a>`
	for _, returning := range []bool{false, true} {
		if returning {
			h.cookies[returningUserCookie] = &http.Cookie{Name: returningUserCookie, Value: "1"}
		}
		for _, hx := range []bool{false, true} {
			login := h.request("GET", loginURL, nil, hx)
			requireStatus(t, login, http.StatusOK)
			if strings.Contains(login.Body.String(), link) {
				t.Fatal("login page links to itself")
			}
			for _, method := range []string{"GET", "POST"} {
				response := h.request(method, "/auth/register/email", url.Values{"email": {"trentnix@gmail.com"}}, hx)
				requireStatus(t, response, http.StatusOK)
				body := response.Body.String()
				if method == "POST" && !strings.Contains(body, "This field is required.") {
					t.Fatal("expected registration validation errors")
				}
				start := strings.Index(body, `id="register-form"`)
				if start < 0 {
					t.Fatal("registration form missing")
				}
				end := strings.Index(body[start:], "</form>")
				if end < 0 {
					t.Fatal("registration form not closed")
				}
				footerEnd := strings.Index(body[start+end:], "</div>")
				linkAt := strings.Index(body, link)
				if strings.Count(body, link) != 1 || linkAt <= start+end || linkAt >= start+end+footerEnd {
					t.Fatal("login link must appear exactly once, below the form inside the registration wrapper")
				}
			}
		}
	}
}

// TestReturningUserCookie checks ordinary and HTMX logins over HTTP and HTTPS.
func TestReturningUserCookie(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	for _, scheme := range []string{"http", "https"} {
		for _, hx := range []bool{false, true} {
			for _, accepted := range []bool{false, true} {
				values := credentials("trentnix@gmail.com")
				if !accepted {
					values.Set("password", "Wrong-Password-123!")
				}
				r := httptest.NewRequest(http.MethodPost, scheme+"://blog.example"+loginURL, strings.NewReader(values.Encode()))
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				if hx {
					r.Header.Set("HX-Request", "true")
				}
				w := httptest.NewRecorder()
				h.app.Handler.ServeHTTP(w, r)
				var remembered *http.Cookie
				for _, cookie := range w.Result().Cookies() {
					if cookie.Name == returningUserCookie {
						remembered = cookie
					}
				}
				if (remembered != nil) != accepted {
					t.Fatalf("scheme=%s HTMX=%v accepted=%v: cookie=%v", scheme, hx, accepted, remembered)
				}
				if remembered != nil && (remembered.Value != "1" || remembered.Path != "/" || remembered.MaxAge != 365*24*60*60 || !remembered.HttpOnly || remembered.Secure != (scheme == "https") || remembered.SameSite != http.SameSiteLaxMode) {
					t.Errorf("unexpected returning-user cookie: %v", remembered)
				}
			}
		}
	}
}

// TestLoginRedirect checks the short login route for ordinary and HTMX navigation.
func TestLoginRedirect(t *testing.T) {
	h := newHarness(t)
	for _, hx := range []bool{false, true} {
		w := h.request(http.MethodGet, "/login", nil, hx)
		status, header := http.StatusSeeOther, "Location"
		if hx {
			status, header = http.StatusOK, "HX-Redirect"
		}
		requireStatus(t, w, status)
		if got := w.Header().Get(header); got != loginURL {
			t.Errorf("HTMX=%v: %s=%q, want %q", hx, header, got, loginURL)
		}
	}
}

// TestPageRenderingFailures checks that handlers report render errors without leaking their cause.
func TestPageRenderingFailures(t *testing.T) {
	h := newHarness(t)
	h.app.ContentManager.LoadNotifications = func(*http.Request) ([]messages.Notification, func(http.ResponseWriter) error, error) {
		return nil, nil, errors.New("private notification failure")
	}
	for _, path := range []string{"/", "/about", "/projects", "/posts"} {
		for _, hx := range []bool{false, true} {
			w := h.request(http.MethodGet, path, nil, hx)
			requireStatus(t, w, http.StatusInternalServerError)
			body := w.Body.String()
			if !strings.Contains(body, "Unable to display this page.") || strings.Contains(body, "private notification failure") {
				t.Errorf("path=%s HTMX=%v: unexpected rendering error response: %s", path, hx, body)
			}
		}
	}
}

// TestNotFoundResponses preserves the blog's message and layout in both response modes.
func TestNotFoundResponses(t *testing.T) {
	h := newHarness(t)
	for _, hx := range []bool{false, true} {
		for _, path := range []string{"/missing", "/post/s/missing"} {
			w := h.request(http.MethodGet, path, nil, hx)
			requireStatus(t, w, http.StatusNotFound)
			body := w.Body.String()
			if !strings.Contains(body, `<div class="page-message" role="status">That page could not be found.</div>`) {
				t.Errorf("path=%s HTMX=%v: missing not-found message in %s", path, hx, body)
			}
			if strings.Contains(body, "<html") == hx {
				t.Errorf("path=%s HTMX=%v: unexpected page layout", path, hx)
			}
		}
	}
}

// TestManagementResponses preserves direct navigation while serving article fragments
// for management links and complete pages for uncached browser-history restores.
func TestManagementResponses(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	for _, page := range []struct{ path, title string }{
		{"/post/manage", "Posts"},
		{"/pages", "Pages"},
		{"/profile", "Site Profile"},
		{"/media", "Media"},
	} {
		for _, mode := range []string{"direct", "fragment", "history"} {
			t.Run(page.path+"/"+mode, func(t *testing.T) {
				r := httptest.NewRequest(http.MethodGet, "https://blog.example"+page.path, nil)
				for _, cookie := range h.cookies {
					r.AddCookie(cookie)
				}
				if mode != "direct" {
					r.Header.Set("HX-Request", "true")
					r.Header.Set("HX-Target", "article-content")
				}
				if mode == "history" {
					r.Header.Set("HX-History-Restore-Request", "true")
				}
				w := httptest.NewRecorder()
				h.app.Handler.ServeHTTP(w, r)
				requireStatus(t, w, http.StatusOK)

				body := w.Body.String()
				if strings.Contains(body, "<html") != (mode != "fragment") || !strings.Contains(body, "<title>Trent Nix | "+page.title+"</title>") {
					t.Fatal("unexpected management layout or document title", body)
				}
				if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("HX-Redirect") != "" {
					t.Fatal("management response must remain private without forcing a reload", w.Header())
				}
			})
		}
	}
}

// TestReadingResponses preserves full-page navigation and limits fragment titles
// to requests that replace the article, not the sidebar or editor.
func TestReadingResponses(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	requireStatus(t, h.request(http.MethodPost, "/post", url.Values{
		"slug": {"reading"}, "title": {"Reading <post>"}, "content": {"Post body"},
	}, false), http.StatusSeeOther)
	h.publish(t, "reading")

	for _, tt := range []struct {
		name, path, target, title string
		hx, restore, full         bool
	}{
		{name: "ordinary post", path: "/post/s/reading", full: true, title: "Reading <post>"},
		{name: "article fragment", path: "/post/s/reading", target: "article-content", hx: true, title: "Reading <post>"},
		{name: "sidebar fragment", path: "/posts/recent", target: "post-archive", hx: true},
		{name: "other fragment", path: "/post/s/reading", hx: true},
		{name: "history restore", path: "/post/s/reading", target: "article-content", hx: true, restore: true, full: true, title: "Reading <post>"},
		{name: "about page", path: "/about", full: true, title: "About Me"},
		{name: "about fragment", path: "/about", target: "article-content", hx: true, title: "About Me"},
		{name: "projects page", path: "/projects", full: true, title: "Projects"},
		{name: "projects fragment", path: "/projects", target: "article-content", hx: true, title: "Projects"},
		{name: "archive page", path: "/posts", full: true, title: "All Posts"},
		{name: "archive fragment", path: "/posts", target: "article-content", hx: true, title: "All Posts"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "https://blog.example"+tt.path, nil)
			r.Header.Set("HX-Target", tt.target)
			if tt.hx {
				r.Header.Set("HX-Request", "true")
			}
			if tt.restore {
				r.Header.Set("HX-History-Restore-Request", "true")
			}
			w := httptest.NewRecorder()
			h.app.Handler.ServeHTTP(w, r)
			requireStatus(t, w, http.StatusOK)

			body := w.Body.String()
			if strings.Contains(body, "<html") != tt.full || strings.Contains(body, "hx-history-elt") != tt.full {
				t.Errorf("full page = %v: unexpected page shell in %s", tt.full, body)
			}
			if strings.Contains(body, "<title>") != (tt.title != "") {
				t.Errorf("title = %q: unexpected title in %s", tt.title, body)
			}
			if tt.title != "" && !strings.Contains(body, "<title>Trent Nix | "+html.EscapeString(tt.title)+"</title>") {
				t.Errorf("missing escaped title %q in %s", tt.title, body)
			}
		})
	}
}

// TestRecentPosts keeps navigation short without hiding entries from the archive.
func TestRecentPosts(t *testing.T) {
	for _, limit := range []int{1, 3, 10, 20} {
		t.Run(fmt.Sprintf("limit=%d", limit), func(t *testing.T) {
			testRecentPosts(t, limit)
		})
	}
}

func testRecentPosts(t *testing.T, limit int) {
	t.Helper()
	h := newHarnessWithRecentPostCount(t, limit)
	h.register(t)
	h.verify(t)
	h.login(t)
	links := regexp.MustCompile(`<li><a href="/post/s/([^"]+)">`)
	created := 0
	for _, count := range []int{0, 1, 10, 12} {
		for created < count {
			created++
			requireStatus(t, h.request(http.MethodPost, "/post", url.Values{
				"slug": {fmt.Sprintf("post-%02d", created)}, "title": {"Post <title>"}, "content": {"Body"},
			}, false), http.StatusSeeOther)
			h.publish(t, fmt.Sprintf("post-%02d", created))
		}
		for _, path := range []string{"/posts/recent", "/posts"} {
			t.Run(fmt.Sprintf("%s/%d", path, count), func(t *testing.T) {
				w := h.request(http.MethodGet, path, nil, true)
				requireStatus(t, w, http.StatusOK)
				body := w.Body.String()
				want := count
				if path == "/posts/recent" {
					want = min(count, limit)
					if !strings.Contains(body, `href="/posts"`) {
						t.Error("recent posts must link to the full archive")
					}
				}
				matches := links.FindAllStringSubmatch(body, -1)
				if len(matches) != want {
					t.Fatalf("got %d post links, want %d", len(matches), want)
				}
				for i, match := range matches {
					if expected := fmt.Sprintf("post-%02d", count-i); match[1] != expected {
						t.Errorf("link %d = %q, want %q", i, match[1], expected)
					}
				}
				if count == 0 && !strings.Contains(body, "No posts yet.") {
					t.Error("missing empty state")
				}
				if count > 0 && !strings.Contains(body, "Post &lt;title&gt;") {
					t.Error("post titles must be escaped")
				}
			})
		}
	}
}

// TestRecentPostCountRequired rejects missing, zero, and negative counts at startup.
func TestRecentPostCountRequired(t *testing.T) {
	for _, count := range []int{0, -1} {
		m := &module{access: &Access{}, settings: &Settings{AdminEmail: "trentnix@gmail.com", RecentPostCount: count}}
		// Invalid settings must fail before accessing application services or storage.
		err := m.Init(t.Context(), nil)
		if err == nil || err.Error() != "blog.recentPostCount must be greater than zero" {
			t.Errorf("count=%d: got %v", count, err)
		}
	}
}

// TestNotificationRendering keeps page and HTMX notifications consistent and escaped.
func TestNotificationRendering(t *testing.T) {
	h := newHarness(t)
	h.app.ContentManager.LoadNotifications = func(*http.Request) ([]messages.Notification, func(http.ResponseWriter) error, error) {
		return []messages.Notification{
			messages.NewNotification("Saved <script>alert(1)</script>", messages.NotificationTypeSuccess),
			messages.NewNotification("Delivery failed", messages.NotificationTypeError),
		}, nil, nil
	}
	for _, hx := range []bool{false, true} {
		response := h.request(http.MethodGet, "/about", nil, hx)
		requireStatus(t, response, http.StatusOK)
		body := response.Body.String()
		for _, want := range []string{
			`class="notification notification--success"`,
			`role="status">Saved &lt;script&gt;alert(1)&lt;/script&gt;`,
			`class="notification">`,
			`role="alert">Delivery failed`,
			`aria-label="Dismiss notification"`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("HTMX=%v: missing %q in %s", hx, want, body)
			}
		}
		if strings.Count(body, `class="notification notification--success"`) != 1 {
			t.Errorf("HTMX=%v: only success notifications should expire", hx)
		}
		if hx && !strings.Contains(body, `hx-swap-oob="beforeend"`) {
			t.Error("HTMX notifications must append to the existing notification region")
		}
	}
}

func (m *mailbox) Send(ctx context.Context, message messaging.MailMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.messages = append(m.messages, message)
	return nil
}

type harness struct {
	app        *application.Application
	blog       *module
	mail       *mailbox
	cookies    map[string]*http.Cookie
	projectsID string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	return newHarnessWithRecentPostCount(t, 10)
}

func newHarnessWithRecentPostCount(t *testing.T, count int) *harness {
	t.Helper()
	directory := t.TempDir()
	moduleDirectory := filepath.Join(directory, "modules", "blog")
	if err := os.MkdirAll(moduleDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	settings := fmt.Sprintf("adminEmail: trentnix@gmail.com\nrecentPostCount: %d\nuploadsDirectory: uploads\nstorage:\n  provider: sqlite\n  connection: posts.db?_journal=WAL\n", count)
	if err := os.WriteFile(filepath.Join(moduleDirectory, "config.yaml"), []byte(settings), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{}
	cfg.App.Name, cfg.App.WorkingDirectory, cfg.App.RenderNotifications = "Trent Nix", directory, true
	cfg.HTTP.ListenHost, cfg.HTTP.Port, cfg.HTTP.PublicOrigin = "127.0.0.1", 8086, "https://blog.example"
	cfg.HTTP.Session.JwtKey = strings.Repeat("s", 40)
	cfg.HTTP.Session.TokenAge, cfg.HTTP.Session.CookieAge = time.Hour, time.Hour
	cfg.HTTP.Session.Stores = map[string]map[string]string{"sqliteStore": {"enabled": "true", "connection": "accounts.db?_journal=WAL", "sessionTable": "sessions"}}
	cfg.HTTP.Session.Types = map[string]string{"default": "sqliteStore"}
	cfg.Auth.Enabled, cfg.Auth.RegistrationEnabled, cfg.Auth.RegisterRequiresVerification = true, true, true
	cfg.Auth.JwtKey = strings.Repeat("a", 40)
	cfg.Auth.VerificationTokenExpiration, cfg.Auth.ResetTokenExpiration = time.Hour, time.Hour
	cfg.Auth.AccountStorage = config.AccountStorageConfig{Provider: "sqlite", Options: map[string]string{"connection": "accounts.db?_journal=WAL"}}
	cfg.Auth.Services = map[string]map[string]string{"email": {"enabled": "true"}}
	h := startHarness(t, cfg)
	if err := h.blog.writeProfile(t.Context(), Profile{Name: "Trent Nix", Bio: "Trent is a technologist interested in building and delivering better software.", BackgroundURL: "/assets/img/site-bg.jpg"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.blog.pageStore.SavePage(t.Context(), Page{Slug: "about", Title: "About Me", DisplayOrder: new(0), Published: true}); err != nil {
		t.Fatal(err)
	}
	projects, err := h.blog.pageStore.SavePage(t.Context(), Page{Slug: "projects", Title: "Projects", Published: true, ChildrenDisplay: "blocks", DisplayOrder: new(1)})
	if err != nil {
		t.Fatal(err)
	}
	h.projectsID = projects.ID
	return h
}

func startHarness(t *testing.T, cfg config.Config) *harness {
	t.Helper()
	h := &harness{mail: &mailbox{}, cookies: make(map[string]*http.Cookie)}
	mail, err := messaging.NewMailClientWithSender("blog@example.test", h.mail)
	if err != nil {
		t.Fatal(err)
	}
	access := &Access{}
	profile := &ProfileState{}
	h.app, err = application.New(t.Context(), cfg, application.Options{
		Services:              application.Services{Mail: mail},
		ContentSecurityPolicy: ContentSecurityPolicy,
		Modules: func(_ context.Context, app *application.Application) ([]handlers.Descriptor, error) {
			if err := ConfigureRendering(app.ContentManager, profile, access, app.Accounts, cfg.Auth.RegistrationEnabled); err != nil {
				return nil, err
			}
			blog := Module(access, profile)
			newBlog := blog.New
			blog.New = func() handlers.Handler {
				h.blog = newBlog().(*module)
				return h.blog
			}
			return []handlers.Descriptor{blog, auth.Module(nil, nil, nil)}, nil
		},
		AuthProviders: func(_ context.Context, app *application.Application) ([]auth.Descriptor, error) {
			return []auth.Descriptor{{Name: "email", New: func() auth.AuthService {
				return NewEmailProvider(app.Accounts, app.Sessions, app.Mail, access)
			}}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := h.app.ShutdownContext(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return h
}

// submitEditor submits a newly opened editor's version. Tests for stale or missing
// versions use request directly so they cannot silently acquire a fresh token.
func (h *harness) submitEditor(t *testing.T, path string, values url.Values, hx bool) *httptest.ResponseRecorder {
	t.Helper()
	if prefix, id, ok := strings.Cut(strings.TrimPrefix(path, "/"), "/"); ok {
		response := h.request("GET", "/"+prefix+"/update/"+id, nil, false)
		requireStatus(t, response, http.StatusOK)
		match := regexp.MustCompile(`name="version" value="([0-9]+)"`).FindStringSubmatch(response.Body.String())
		if len(match) != 2 {
			t.Fatal("editor has no version token")
		}
		values.Set("version", match[1])
	}
	return h.request("POST", path, values, hx)
}

func (h *harness) request(method, path string, values url.Values, hx bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://blog.example"+path, strings.NewReader(values.Encode()))
	if values != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if hx {
		r.Header.Set("HX-Request", "true")
	}
	for _, cookie := range h.cookies {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.app.Handler.ServeHTTP(w, r)
	for _, cookie := range w.Result().Cookies() {
		if cookie.MaxAge < 0 {
			delete(h.cookies, cookie.Name)
		} else {
			h.cookies[cookie.Name] = cookie
		}
	}
	return w
}

func credentials(email string) url.Values {
	return url.Values{"email": {email}, "password": {"Sample-Password-123!"}, "passwordMatch": {"Sample-Password-123!"}}
}

func requireStatus(t *testing.T, w *httptest.ResponseRecorder, status int) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status=%d want=%d body=%s", w.Code, status, w.Body.String())
	}
}

func (h *harness) register(t *testing.T) {
	t.Helper()
	requireStatus(t, h.request("POST", "/auth/register/email", credentials("TrentNix@gmail.com"), false), http.StatusSeeOther)
	if len(h.mail.messages) != 1 || h.mail.messages[0].To != "TrentNix@gmail.com" {
		t.Fatal("registration must send one message to the configured account")
	}
}

func (h *harness) verify(t *testing.T) {
	t.Helper()
	link := regexp.MustCompile(`https://blog.example/auth/verify\?[^"< ]+`).FindString(h.mail.messages[0].Body)
	u, err := url.Parse(html.UnescapeString(link))
	if err != nil || u.Query().Get("token") == "" {
		t.Fatalf("missing verification link: %v", err)
	}
	requireStatus(t, h.request("POST", "/auth/verify", url.Values{"token": {u.Query().Get("token")}}, false), http.StatusSeeOther)
}

func (h *harness) login(t *testing.T) {
	t.Helper()
	requireStatus(t, h.request("POST", "/auth/login/email", credentials("trentnix@gmail.com"), false), http.StatusSeeOther)
}

func TestAuthenticationPagesAndAdmission(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{"/", "/about", "/projects", "/posts", "/auth/login/email", "/auth/register/email", "/auth/reset/request/email"} {
		w := h.request("GET", path, nil, false)
		requireStatus(t, w, http.StatusOK)
		if !strings.Contains(w.Body.String(), "/assets/css/blog.css") || !strings.Contains(w.Body.String(), "profile-name") {
			t.Errorf("%s lacks blog layout", path)
		}
	}
	requireStatus(t, h.request("GET", "/missing", nil, false), http.StatusNotFound)
	requireStatus(t, h.request("GET", "/setup", nil, false), http.StatusNotFound)
	if !strings.Contains(h.request("GET", "/", nil, false).Body.String(), `href="/auth/register/email">Register</a>`) {
		t.Fatal("registration link is missing")
	}
	requireStatus(t, h.request("GET", "/post/create", nil, false), http.StatusForbidden)
	for _, hx := range []bool{false, true} {
		for _, path := range []string{"/auth/register/email", "/auth/login/email"} {
			w := h.request("POST", path+"?email=trentnix@gmail.com", credentials("stranger@example.test"), hx)
			requireStatus(t, w, http.StatusForbidden)
			if strings.Contains(w.Body.String(), "<html") == hx {
				t.Fatal("incorrect full-page/fragment response")
			}
		}
	}
	if _, err := h.app.Accounts.GetByEmail(t.Context(), "stranger@example.test"); !errors.Is(err, user.ErrUserNotFound) {
		t.Fatalf("disallowed account exists: %v", err)
	}
	if len(h.mail.messages) != 0 {
		t.Fatal("rejected registration sent mail")
	}
	h.register(t)
	h.login(t)
	requireStatus(t, h.request("GET", "/post/create", nil, false), http.StatusForbidden)
	w := h.request("GET", "/auth/request/verify", nil, false)
	requireStatus(t, w, http.StatusOK)
	if !strings.Contains(w.Body.String(), "verification-panel") {
		t.Fatal("verification page lacks themed panel")
	}
	h.verify(t)
	requireStatus(t, h.request("GET", "/post/create", nil, false), http.StatusOK)
}

func TestRegistrationCanBeDisabledWithoutAdministrator(t *testing.T) {
	h := newHarness(t)
	cfg := *h.app.Config
	cfg.Auth.RegistrationEnabled = false
	if err := h.app.ShutdownContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	h = startHarness(t, cfg)
	w := h.request("GET", "/", nil, false)
	requireStatus(t, w, http.StatusOK)
	if strings.Contains(w.Body.String(), `href="/auth/register/email"`) {
		t.Fatal("disabled registration is still linked")
	}
	requireStatus(t, h.request("GET", "/setup", nil, false), http.StatusNotFound)
	if _, err := h.app.Accounts.GetByEmail(t.Context(), "trentnix@gmail.com"); !errors.Is(err, user.ErrUserNotFound) {
		t.Fatalf("unexpected administrator account: %v", err)
	}
}

// TestRegistrationLinkDisappears checks persisted account state before verification
// and after an application restart, without changing registration configuration.
func TestRegistrationLinkDisappears(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	check := func() {
		t.Helper()
		for _, path := range []string{"/", "/auth/login/email"} {
			w := h.request(http.MethodGet, path, nil, false)
			requireStatus(t, w, http.StatusOK)
			if strings.Contains(w.Body.String(), `href="/auth/register/email"`) {
				t.Errorf("%s still links registration after the administrator registered", path)
			}
		}
	}
	check()
	cfg := *h.app.Config
	if err := h.app.ShutdownContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	h = startHarness(t, cfg)
	check()
}

// unavailableAccounts simulates a lookup failure without changing account storage.
type unavailableAccounts struct{ user.AccountRepository }

// GetByEmail reports an unavailable store, not an absent administrator.
func (unavailableAccounts) GetByEmail(context.Context, string) (*user.User, error) {
	return nil, errors.New("account storage unavailable")
}

// TestRegistrationLinkHiddenOnLookupFailure keeps storage errors from advertising signup.
func TestRegistrationLinkHiddenOnLookupFailure(t *testing.T) {
	cm := contentservice.NewContentManager()
	if err := ConfigureRendering(cm, &ProfileState{}, &Access{emailKey: user.EmailKey("trentnix@gmail.com")}, unavailableAccounts{}, true); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if cm.BuildLayoutData(r).(navigation).Registration {
		t.Fatal("registration link shown when account lookup failed")
	}
}

func TestPublishingAndRecoveryPaths(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	post := url.Values{"title": {"First <script>"}, "slug": {"first-post"}, "summary": {"Summary & details"}, "content": {"# Hello\n<script>alert(1)</script>\n\n[bad](javascript:alert(1))"}}
	w := h.request("POST", "/post", post, true)
	requireStatus(t, w, http.StatusOK)
	if w.Header().Get("HX-Redirect") != "/post/s/first-post" {
		t.Fatal("missing HTMX redirect")
	}
	w = h.request("GET", "/post/s/first-post", nil, false)
	requireStatus(t, w, http.StatusOK)
	if strings.Contains(w.Body.String(), "<script>alert(1)") || strings.Contains(w.Body.String(), `href="javascript:`) || !strings.Contains(w.Body.String(), "First &lt;script&gt;") {
		t.Fatal("post HTML is unsafe")
	}
	match := regexp.MustCompile(`/post/update/([A-Za-z0-9]+)`).FindStringSubmatch(w.Body.String())
	if len(match) != 2 {
		t.Fatal("missing edit link")
	}
	id := match[1]
	h.publish(t, "first-post")
	requireStatus(t, h.request("POST", "/post", post, true), http.StatusConflict)
	post.Set("title", "")
	w = h.submitEditor(t, "/post/"+id, post, true)
	requireStatus(t, w, http.StatusUnprocessableEntity)
	if strings.Contains(w.Body.String(), "post-form") {
		t.Fatal("validation replaces the editor and loses its draft")
	}
	post.Set("title", "Edited")
	post.Set("published", "on")
	requireStatus(t, h.submitEditor(t, "/post/"+id, post, false), http.StatusSeeOther)
	w = h.request("GET", "/rss", nil, false)
	requireStatus(t, w, http.StatusOK)
	var feed feed
	if err := xml.Unmarshal(w.Body.Bytes(), &feed); err != nil || len(feed.Channel.Items) != 1 || feed.Channel.Items[0].Title != "Edited" {
		t.Fatalf("invalid RSS: %v", err)
	}
	if feed.Channel.Items[0].Link != "https://blog.example/post/s/first-post" {
		t.Fatal("incorrect public feed URL")
	}
	requireStatus(t, h.request("POST", "/post/preview", url.Values{"content": {strings.Repeat("x", maxMarkdownBytes+1)}}, false), http.StatusRequestEntityTooLarge)
	requireStatus(t, h.request("POST", "/post/delete/"+id, url.Values{}, false), http.StatusSeeOther)
	requireStatus(t, h.request("POST", "/post/"+id, post, false), http.StatusNotFound)
	requireStatus(t, h.request("GET", "/post/id/"+id, nil, false), http.StatusNotFound)
}

func TestImageUpload(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		data   []byte
		status int
	}{
		{"valid", pngData.Bytes(), http.StatusOK},
		{"svg", []byte(`<svg onload="alert(1)"></svg>`), http.StatusUnprocessableEntity},
		{"truncated", pngData.Bytes()[:len(pngData.Bytes())-8], http.StatusUnprocessableEntity},
		{"oversized", make([]byte, maxImageBytes+1), http.StatusUnprocessableEntity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			part, err := writer.CreateFormFile("image", "../../picture.png")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := part.Write(tc.data); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("POST", "https://blog.example/post/images/upload", &body)
			r.Header.Set("Content-Type", writer.FormDataContentType())
			for _, cookie := range h.cookies {
				r.AddCookie(cookie)
			}
			w := httptest.NewRecorder()
			h.app.Handler.ServeHTTP(w, r)
			requireStatus(t, w, tc.status)
			if tc.status != http.StatusOK {
				return
			}
			var result struct{ Data struct{ FilePath string } }
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			id := strings.TrimPrefix(result.Data.FilePath, "/images/")
			metadata, err := h.blog.mediaMetadata.GetMediaMetadata(t.Context(), id)
			if err != nil || metadata.Filename != "picture.png" {
				t.Fatal("upload filename was not retained safely", metadata, err)
			}
			saved, err := h.blog.images.OpenVariant(t.Context(), id, originalImage)
			if err != nil {
				t.Fatalf("uploaded image was not saved in the configured directory: %v", err)
			}
			original, err := io.ReadAll(saved.Content)
			saved.Content.Close()
			if err != nil || !bytes.Equal(original, tc.data) {
				t.Fatalf("original changed: %v", err)
			}
			prepared, err := prepareImage(t.Context(), tc.data)
			if err != nil {
				t.Fatal(err)
			}
			w = h.request("GET", result.Data.FilePath, nil, false)
			requireStatus(t, w, http.StatusOK)
			if !bytes.Equal(w.Body.Bytes(), prepared.Display) || w.Header().Get("Content-Type") != "image/png" {
				t.Fatal("image roundtrip failed")
			}
			w = h.request("HEAD", result.Data.FilePath, nil, false)
			if w.Body.Len() != 0 {
				t.Fatal("HEAD returned an image body")
			}
		})
	}
}

func TestMailFailurePreservesVerificationRequirement(t *testing.T) {
	h := newHarness(t)
	h.mail.err = errors.New("mail unavailable")
	w := h.request("POST", "/auth/register/email", credentials("trentnix@gmail.com"), false)
	if !strings.Contains(w.Body.String(), "verification-panel") || !strings.Contains(w.Body.String(), "could not send") {
		t.Fatal("missing styled delivery failure")
	}
	u, err := h.app.Accounts.GetByEmail(t.Context(), "trentnix@gmail.com")
	if err != nil || u.Verified {
		t.Fatalf("registration failure bypassed verification: %v", err)
	}
	h.login(t)
	requireStatus(t, h.request("GET", "/post/create", nil, false), http.StatusForbidden)
	h.mail.err = nil
	requireStatus(t, h.request("POST", "/auth/request/verify", nil, false), http.StatusOK)
	if len(h.mail.messages) != 1 {
		t.Fatal("verification resend did not deliver instructions")
	}
	h.verify(t)
	requireStatus(t, h.request("GET", "/", nil, false), http.StatusOK)
}

type failingRepository struct{ repository }

func (failingRepository) Save(context.Context, Post) (Post, error) {
	return Post{}, errors.New("private storage failure")
}

func TestSaveFailurePreservesDraft(t *testing.T) {
	h := newHarness(t)
	templates, err := prepareTemplates(h.app.ContentManager)
	if err != nil {
		t.Fatal(err)
	}
	m := &module{app: h.app.ApplicationServer, posts: failingRepository{}, views: make(map[string]*content.View)}
	for _, name := range []string{"editor", "message"} {
		m.views[name], err = templates.View(h.app.ContentManager, "blog."+name)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, hx := range []bool{false, true} {
		r := httptest.NewRequest("POST", "/post", strings.NewReader(url.Values{"title": {"Unsaved title"}, "content": {"Unsaved Markdown"}}.Encode()))
		r = session.AddSessionManagerToRequestContext(r, h.app.Sessions)
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if hx {
			r.Header.Set("HX-Request", "true")
		}
		w := httptest.NewRecorder()
		m.save(w, r)
		requireStatus(t, w, http.StatusInternalServerError)
		if strings.Contains(w.Body.String(), "private storage") {
			t.Fatal("storage details exposed")
		}
		if !hx && !strings.Contains(w.Body.String(), "Unsaved Markdown") {
			t.Fatal("ordinary failure lost the draft")
		}
		if hx && strings.Contains(w.Body.String(), "post-form") {
			t.Fatal("HTMX failure replaced the editor")
		}
	}
}

func TestPublishingAuthorizationAndCSRF(t *testing.T) {
	h := newHarness(t)
	paths := []string{"/post", "/post/example", "/post/delete/example", "/post/preview", "/post/images/upload"}
	for _, path := range paths {
		requireStatus(t, h.request("POST", path, url.Values{}, false), http.StatusForbidden)
	}
	h.register(t)
	h.login(t)
	for _, path := range paths {
		requireStatus(t, h.request("POST", path, url.Values{}, false), http.StatusForbidden)
	}
	h.verify(t)
	r := httptest.NewRequest("POST", "https://blog.example/post", strings.NewReader("title=Hello&content=World"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://attacker.example")
	for _, cookie := range h.cookies {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.app.Handler.ServeHTTP(w, r)
	requireStatus(t, w, http.StatusForbidden)
}

func TestImagePixelLimit(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	data := encoded.Bytes()
	// Alter the PNG IHDR without allocating an oversized pixel buffer.
	binary.BigEndian.PutUint32(data[16:20], 5000)
	binary.BigEndian.PutUint32(data[20:24], 5000)
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	if _, err := prepareImage(t.Context(), data); err == nil || !strings.Contains(err.Error(), "20 million") {
		t.Fatalf("pixel limit not enforced: %v", err)
	}
}

type navigationRepository struct {
	repository
	latest  Post
	entries []ArchiveEntry
	err     error
}

func (s navigationRepository) Latest(context.Context) (Post, error) { return s.latest, s.err }
func (s navigationRepository) NextPublication(context.Context) (time.Time, error) {
	return time.Time{}, s.err
}
func (s navigationRepository) ListArchive(context.Context) ([]ArchiveEntry, error) {
	return s.entries, s.err
}
func (s navigationRepository) ListArchivePage(_ context.Context, limit, offset int) ([]ArchiveEntry, error) {
	start := min(offset, len(s.entries))
	return s.entries[start:min(start+limit, len(s.entries))], s.err
}
func (s navigationRepository) ListRecent(_ context.Context, limit int) ([]ArchiveEntry, error) {
	return s.entries[:min(len(s.entries), limit)], s.err
}

func TestNavigationHandlers(t *testing.T) {
	h := newHarness(t)
	templates, err := prepareTemplates(h.app.ContentManager)
	if err != nil {
		t.Fatal(err)
	}
	m := &module{app: h.app.ApplicationServer, settings: &Settings{RecentPostCount: 3}, views: make(map[string]*content.View)}
	for _, name := range []string{"post", "archive", "recent", "message"} {
		m.views[name], err = templates.View(h.app.ContentManager, "blog."+name)
		if err != nil {
			t.Fatal(err)
		}
	}
	date := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		store   navigationRepository
		status  int
		text    string
	}{
		{"empty home", m.index, navigationRepository{err: errNotFound}, http.StatusOK, "No posts yet."},
		{"latest post", m.index, navigationRepository{latest: Post{Title: "Newest post", Markdown: "Latest body", Published: date}}, http.StatusOK, "Latest body"},
		{"empty archive", m.archive, navigationRepository{}, http.StatusOK, "No posts yet."},
		{"empty recent posts", m.recent, navigationRepository{}, http.StatusOK, "No posts yet."},
		{"recent entries", m.recent, navigationRepository{entries: []ArchiveEntry{{Slug: "newest", Title: "Newest post", Published: date}}}, http.StatusOK, `/post/s/newest`},
		{"archive entries", m.archive, navigationRepository{entries: []ArchiveEntry{{Slug: "older", Title: "Older post", Published: date}}}, http.StatusOK, `/post/s/older`},
		{"home storage failure", m.index, navigationRepository{err: errors.New("private storage failure")}, http.StatusInternalServerError, "Unable to access blog storage"},
		{"archive storage failure", m.archive, navigationRepository{err: errors.New("private storage failure")}, http.StatusInternalServerError, "Unable to access blog storage"},
		{"recent posts storage failure", m.recent, navigationRepository{err: errors.New("private storage failure")}, http.StatusInternalServerError, "Unable to access blog storage"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// List is deliberately unavailable: neither navigation handler needs full post collections.
			m.posts = tc.store
			m.recentPosts.invalidate()
			r := session.AddSessionManagerToRequestContext(httptest.NewRequest("GET", "/", nil), h.app.Sessions)
			w := httptest.NewRecorder()
			tc.handler(w, r)
			requireStatus(t, w, tc.status)
			if !strings.Contains(w.Body.String(), tc.text) {
				t.Fatalf("missing %q in response", tc.text)
			}
			if strings.Contains(w.Body.String(), "private storage") {
				t.Fatal("storage error exposed")
			}
		})
	}
}
