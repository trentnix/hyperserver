package blog

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"errors"
	"hash/crc32"
	"html"
	"image"
	"image/png"
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
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

type mailbox struct {
	mu       sync.Mutex
	messages []messaging.MailMessage
	err      error
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
	app     *application.Application
	mail    *mailbox
	cookies map[string]*http.Cookie
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	directory := t.TempDir()
	moduleDirectory := filepath.Join(directory, "modules", "blog")
	if err := os.MkdirAll(moduleDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDirectory, "config.yaml"), []byte("adminEmail: trentnix@gmail.com\nstorage:\n  provider: sqlite\n  connection: posts.db?_journal=WAL\n"), 0600); err != nil {
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
	return startHarness(t, cfg)
}

func startHarness(t *testing.T, cfg config.Config) *harness {
	t.Helper()
	h := &harness{mail: &mailbox{}, cookies: make(map[string]*http.Cookie)}
	mail, err := messaging.NewMailClientWithSender("blog@example.test", h.mail)
	if err != nil {
		t.Fatal(err)
	}
	access := &Access{}
	var services *application.Application
	h.app, err = application.New(t.Context(), cfg, application.Options{
		Services:              application.Services{Mail: mail},
		ContentSecurityPolicy: ContentSecurityPolicy,
		Modules: func(_ context.Context, app *application.Application) ([]handlers.Descriptor, error) {
			services = app
			if err := ConfigureRendering(app.ContentManager, access, cfg.Auth.RegistrationEnabled); err != nil {
				return nil, err
			}
			return []handlers.Descriptor{Module(access), auth.Module(nil, nil, nil)}, nil
		},
		AuthProviders: []auth.Descriptor{{Name: "email", New: func() auth.AuthService {
			return NewEmailProvider(services.Accounts, services.Sessions, services.Mail, access)
		}}},
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
	requireStatus(t, h.request("POST", "/post", post, true), http.StatusConflict)
	post.Set("title", "")
	w = h.request("POST", "/post/"+id, post, true)
	requireStatus(t, w, http.StatusUnprocessableEntity)
	if strings.Contains(w.Body.String(), "post-form") {
		t.Fatal("validation replaces the editor and loses its draft")
	}
	post.Set("title", "Edited")
	requireStatus(t, h.request("POST", "/post/"+id, post, false), http.StatusSeeOther)
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
			part, err := writer.CreateFormFile("image", "picture.png")
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
			w = h.request("GET", result.Data.FilePath, nil, false)
			requireStatus(t, w, http.StatusOK)
			if !bytes.Equal(w.Body.Bytes(), tc.data) || w.Header().Get("Content-Type") != "image/png" {
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
	templates, err := content.PrepareTemplates(h.app.ContentManager, content.TemplatePath{FS: blogModuleFS, Path: pagesFile})
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
	if _, err := imageMedia(data); err == nil || !strings.Contains(err.Error(), "20 million") {
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
func (s navigationRepository) ListArchive(context.Context) ([]ArchiveEntry, error) {
	return s.entries, s.err
}

func TestNavigationHandlers(t *testing.T) {
	h := newHarness(t)
	templates, err := content.PrepareTemplates(h.app.ContentManager, content.TemplatePath{FS: blogModuleFS, Path: pagesFile})
	if err != nil {
		t.Fatal(err)
	}
	m := &module{app: h.app.ApplicationServer, views: make(map[string]*content.View)}
	for _, name := range []string{"post", "archive", "message"} {
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
		{"archive entries", m.archive, navigationRepository{entries: []ArchiveEntry{{Slug: "older", Title: "Older post", Published: date}}}, http.StatusOK, `/post/s/older`},
		{"home storage failure", m.index, navigationRepository{err: errors.New("private storage failure")}, http.StatusInternalServerError, "Unable to access blog storage"},
		{"archive storage failure", m.archive, navigationRepository{err: errors.New("private storage failure")}, http.StatusInternalServerError, "Unable to access blog storage"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// List is deliberately unavailable: neither navigation handler needs full post collections.
			m.posts = tc.store
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
