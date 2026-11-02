package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
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
	"testing"

	"github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/internal/maintenance"
	"github.com/trentnix/hyperserver/modules/blog"
	"github.com/trentnix/hyperserver/pkg/application"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
)

type discardMail struct{}

func (discardMail) Send(context.Context, messaging.MailMessage) error { return nil }

// recoveryApp uses the blog's public module contracts, matching its application harness.
func recoveryApp(t *testing.T, cfg config.Config) (*application.Application, func()) {
	t.Helper()
	lock, err := maintenance.Lock(cfg.App.WorkingDirectory)
	if err != nil {
		t.Fatal(err)
	}
	mail, err := messaging.NewMailClientWithSender("blog@example.test", discardMail{})
	if err != nil {
		t.Fatal(err)
	}
	site := &blog.Site{}
	app, err := application.New(t.Context(), cfg, application.Options{
		Services:              application.Services{Mail: mail},
		ContentSecurityPolicy: blog.ContentSecurityPolicy,
		Modules: func(_ context.Context, app *application.Application) ([]handlers.Descriptor, error) {
			module, err := site.Configure(app)
			if err != nil {
				return nil, err
			}
			return []handlers.Descriptor{module, auth.Module(nil, nil, nil)}, nil
		},
		AuthProviders: func(_ context.Context, app *application.Application) ([]auth.Descriptor, error) {
			return []auth.Descriptor{site.EmailProvider(app)}, nil
		},
	})
	if err != nil {
		lock.Close()
		t.Fatal(err)
	}
	stop := func() {
		if err := app.ShutdownContext(context.Background()); err != nil {
			t.Error(err)
		}
		lock.Close()
	}
	t.Cleanup(stop)
	return app, stop
}

// TestApplicationRecovery exercises accounts, sessions, published and draft writing,
// revision history, profile content, media tags, and image visibility after restoration.
func TestApplicationRecovery(t *testing.T) {
	cfg := fixture(t)
	// The archive unit fixture uses arbitrary bytes. The running application
	// needs real image bundles, which this test creates through its upload route.
	if err := os.Remove(filepath.Join(cfg.App.WorkingDirectory, "uploads", "test.png")); err != nil {
		t.Fatal(err)
	}
	cfg.HTTP.PublicOrigin = "https://blog.example"
	app, stop := recoveryApp(t, cfg)
	cookies := make(map[string]*http.Cookie)
	request := func(method, path string, body io.Reader, contentType string, status int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "https://blog.example"+path, body)
		if contentType != "" {
			r.Header.Set("Content-Type", contentType)
		}
		for _, cookie := range cookies {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		app.Handler.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: status %d, want %d: %s", method, path, w.Code, status, w.Body.String())
		}
		for _, cookie := range w.Result().Cookies() {
			if cookie.MaxAge < 0 {
				delete(cookies, cookie.Name)
			} else {
				cookies[cookie.Name] = cookie
			}
		}
		return w
	}
	form := func(path string, values url.Values) *httptest.ResponseRecorder {
		t.Helper()
		return request("POST", path, strings.NewReader(values.Encode()), "application/x-www-form-urlencoded", http.StatusSeeOther)
	}
	get := func(path string) string {
		t.Helper()
		return request("GET", path, nil, "", http.StatusOK).Body.String()
	}
	credentials := url.Values{"email": {"admin@example.com"}, "password": {"Recovery-Test-123!"}, "passwordMatch": {"Recovery-Test-123!"}}
	form("/auth/register/email", credentials)
	account, err := app.Accounts.GetByEmail(t.Context(), "admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	account.SetVerified()
	if err := app.Accounts.Update(t.Context(), account); err != nil {
		t.Fatal(err)
	}
	form("/auth/login/email", credentials)
	request("GET", "/", nil, "", http.StatusSeeOther) // Follow the first login into Site Profile.
	get("/profile")
	upload := func(red uint8) string {
		t.Helper()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("image", "test.png")
		if err != nil {
			t.Fatal(err)
		}
		img := image.NewRGBA(image.Rect(0, 0, 3, 3))
		img.Set(0, 0, color.RGBA{R: red, A: 255})
		if err := png.Encode(part, img); err != nil {
			t.Fatal(err)
		}
		writer.Close()
		w := request("POST", "/post/images/upload", &body, writer.FormDataContentType(), http.StatusOK)
		var result struct{ Data struct{ FilePath string } }
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result.Data.FilePath
	}
	publicImage, privateImage := upload(100), upload(200)
	public := url.Values{"title": {"Original title"}, "slug": {"recovered"}, "content": {"Public body ![Image](" + publicImage + ")"}, "tags": {"Original tag"}, "published": {"on"}}
	form("/post", public)
	page := get("/post/s/recovered")
	match := regexp.MustCompile(`/post/update/([a-zA-Z0-9]+)`).FindStringSubmatch(page)
	if len(match) != 2 {
		t.Fatal("post edit link missing")
	}
	postID := match[1]
	editor := get("/post/update/" + postID)
	version := regexp.MustCompile(`name="version" value="([0-9]+)"`).FindStringSubmatch(editor)
	if len(version) != 2 {
		t.Fatal("post version missing")
	}
	public.Set("version", version[1])
	public.Set("title", "Recovered title")
	public.Set("tags", "recovery, public")
	form("/post/"+postID, public)
	form("/post", url.Values{"title": {"Private draft"}, "slug": {"private"}, "content": {"![Private](" + privateImage + ")"}, "tags": {"private-tag"}})
	form("/pages", url.Values{"title": {"Recovered page"}, "slug": {"recovered-page"}, "content": {"Page body"}, "tags": {"page-tag"}, "childrenDisplay": {"hidden"}, "published": {"on"}})
	form("/profile", url.Values{"version": {"0"}, "name": {"Recovered site"}, "bio": {"Recovered biography"}})
	mediaID := strings.TrimPrefix(privateImage, "/images/")
	form("/media/"+mediaID, url.Values{"version": {"1"}, "label": {"Private image"}, "tags": {"media-tag"}})
	archive := filepath.Join(t.TempDir(), "recovery.zip")
	if err := Create(t.Context(), cfg, archive); err == nil {
		t.Fatal("backup accepted a running application")
	}
	stop()
	if err := Create(t.Context(), cfg, archive); err != nil {
		t.Fatal(err)
	}
	directory := recoverFixture(t, archive, cfg)
	// The source installation remains present and unchanged.
	if _, err := os.Stat(filepath.Join(cfg.App.WorkingDirectory, "blog.db")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)
	restored, err := config.GetConfig()
	if err != nil {
		t.Fatal(err)
	}
	app, _ = recoveryApp(t, restored)
	// Existing session cookies still identify the same verified administrator.
	if !strings.Contains(get("/post/update/"+postID), "Recovered title") {
		t.Fatal("restored session or writing lost")
	}
	for _, tc := range []struct{ path, text string }{
		{"/post/s/recovered", "Recovered title"},
		{"/history/post/" + postID, "Original title"},
		{"/page/recovered-page", "Page body"},
		{"/profile", "Recovered biography"},
		{"/post/manage?q=private-tag", "Private draft"},
		{"/media?q=media-tag", "Private image"},
	} {
		if !strings.Contains(get(tc.path), tc.text) {
			t.Fatalf("restored %s missing %q", tc.path, tc.text)
		}
	}
	get(privateImage)
	cookies = make(map[string]*http.Cookie)
	get(publicImage)
	request("GET", privateImage, nil, "", http.StatusNotFound)
	request("GET", "/post/s/private", nil, "", http.StatusNotFound)
	form("/auth/login/email", credentials)
	get("/media")
}
