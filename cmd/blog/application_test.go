package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/modules/blog"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

const adminEmail = "writer@example.com"
const adminPassword = "Blog-test-password1!"

func testSettings(t *testing.T) settings {
	t.Helper()
	return settings{dataDir: t.TempDir(), publicOrigin: "http://blog.test", title: "Test blog", description: "Software & ideas",
		adminEmail: adminEmail, authKey: strings.Repeat("a", 32), sessionKey: strings.Repeat("b", 32)}
}

type browser struct {
	handler http.Handler
	jar     http.CookieJar
	hx      bool
}

func newBrowser(app *application, hx bool) *browser {
	jar, _ := cookiejar.New(nil)
	return &browser{newHTTPServer(app).Handler, jar, hx}
}

func (b *browser) raw(method, path, media string, body io.Reader) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://blog.test"+path, body)
	if media != "" {
		r.Header.Set("Content-Type", media)
	}
	if b.hx {
		r.Header.Set("HX-Request", "true")
	}
	for _, cookie := range b.jar.Cookies(r.URL) {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	b.handler.ServeHTTP(w, r)
	b.jar.SetCookies(r.URL, w.Result().Cookies())
	return w
}

func (b *browser) request(method, path string, values url.Values) *httptest.ResponseRecorder {
	if values == nil {
		return b.raw(method, path, "", nil)
	}
	return b.raw(method, path, "application/x-www-form-urlencoded", strings.NewReader(values.Encode()))
}

func assertRedirect(t *testing.T, w *httptest.ResponseRecorder, path string, hx bool) {
	t.Helper()
	if hx {
		if w.Code != 200 || w.Header().Get("HX-Redirect") != path {
			t.Fatalf("HTMX redirect = %d %v %s", w.Code, w.Header(), w.Body)
		}
	} else if w.Code != 303 || w.Header().Get("Location") != path {
		t.Fatalf("redirect = %d %v %s", w.Code, w.Header(), w.Body)
	}
}

func startTest(t *testing.T, s settings) *application {
	t.Helper()
	app, err := newApplication(t.Context(), s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Shutdown(); err != nil {
			t.Error(err)
		}
	})
	return app
}

func login(t *testing.T, b *browser, email string, target ...string) {
	t.Helper()
	w := b.request("POST", "/auth/login/email", url.Values{"email": {email}, "password": {adminPassword}})
	want := "/"
	if len(target) > 0 {
		want = target[0]
	}
	assertRedirect(t, w, want, b.hx)
}

func TestBlogHTTPFlow(t *testing.T) {
	for _, hx := range []bool{false, true} {
		t.Run(fmt.Sprint("htmx=", hx), func(t *testing.T) {
			app := startTest(t, testSettings(t))
			b := newBrowser(app, hx)
			if err := createAdmin(t.Context(), app.accounts, adminEmail, adminPassword); err != nil {
				t.Fatal(err)
			}
			w := b.request("GET", "/", nil)
			if w.Code != 200 || !strings.Contains(w.Body.String(), "first post") || strings.Contains(w.Body.String(), "<!doctype html>") == hx {
				t.Fatalf("home = %d %s", w.Code, w.Body)
			}
			assertRedirect(t, b.request("GET", "/post/create", nil), "/auth/login/email", hx)
			login(t, b, strings.ToUpper(adminEmail), "/post/create")
			w = b.request("GET", "/post/create", nil)
			if w.Code != 200 || !strings.Contains(w.Body.String(), `name="markdown"`) {
				t.Fatalf("editor = %d %s", w.Code, w.Body)
			}
			values := url.Values{"title": {"First <post>"}, "summary": {"Software & thoughts"}, "markdown": {"# Hello\n\n**world**\n\n<script>alert(1)</script>"}}
			assertRedirect(t, b.request("POST", "/post", values), "/post/s/first-post", hx)
			w = b.request("GET", "/post/s/first-post", nil)
			if w.Code != 200 || !strings.Contains(w.Body.String(), "<strong>world</strong>") || strings.Contains(w.Body.String(), "<script>alert") || !strings.Contains(w.Body.String(), "First &lt;post&gt;") {
				t.Fatalf("post = %d %s", w.Code, w.Body)
			}
			match := regexp.MustCompile(`/post/update/([a-f0-9-]+)`).FindStringSubmatch(w.Body.String())
			if len(match) != 2 {
				t.Fatal("missing edit link", w.Body)
			}
			id := match[1]
			w = b.request("POST", "/post", values)
			if w.Code != 200 || !strings.Contains(w.Body.String(), blog.ErrSlugExists.Error()) {
				t.Fatalf("duplicate = %d %s", w.Code, w.Body)
			}
			values.Set("version", "1")
			values.Set("title", "Revised")
			values.Set("slug", "revised")
			assertRedirect(t, b.request("POST", "/post/update/"+id, values), "/post/s/revised", hx)
			w = b.request("POST", "/post/update/"+id, values)
			if w.Code != 200 || !strings.Contains(w.Body.String(), blog.ErrConflict.Error()) {
				t.Fatalf("stale = %d %s", w.Code, w.Body)
			}
			if w := b.request("GET", "/post/s/first-post", nil); w.Code != 404 {
				t.Fatal("old slug still exists", w.Code)
			}
			if w := b.request("GET", "/post/id/"+id, nil); w.Code != 200 {
				t.Fatal("stable permalink failed", w.Code)
			}
			w = b.request("GET", "/posts", nil)
			if w.Code != 200 || !strings.Contains(w.Body.String(), "Revised") {
				t.Fatal("archive", w.Code, w.Body)
			}
			w = b.request("GET", "/rss", nil)
			var feed struct {
				Channel struct {
					Title string
					Items []struct {
						Link string `xml:"link"`
					} `xml:"item"`
				} `xml:"channel"`
			}
			if err := xml.Unmarshal(w.Body.Bytes(), &feed); err != nil || len(feed.Channel.Items) != 1 || feed.Channel.Items[0].Link != "http://blog.test/post/s/revised" {
				t.Fatal("RSS", w.Body, err)
			}
			if w := b.request("POST", "/post/delete/"+id, url.Values{"version": {"1"}}); w.Code != 409 {
				t.Fatal("stale delete", w.Code, w.Body)
			}
			assertRedirect(t, b.request("POST", "/post/delete/"+id, url.Values{"version": {"2"}}), "/", hx)
			if w := b.request("GET", "/post/id/"+id, nil); w.Code != 404 {
				t.Fatal("deleted post", w.Code)
			}
			assertRedirect(t, b.request("POST", "/auth/logout", url.Values{}), "/", hx)
			assertRedirect(t, b.request("GET", "/post/create", nil), "/auth/login/email", hx)
		})
	}
}

func TestAuthorizationAndRejections(t *testing.T) {
	app := startTest(t, testSettings(t))
	b := newBrowser(app, false)
	for _, path := range []string{"/post", "/post/update/123", "/post/delete/123", "/post/images/upload"} {
		assertRedirect(t, b.request("POST", path, url.Values{}), "/auth/login/email", false)
	}
	if err := createAdmin(t.Context(), app.accounts, "reader@example.com", adminPassword); err != nil {
		t.Fatal(err)
	}
	login(t, b, "reader@example.com", "/post/images/upload")
	for _, path := range []string{"/post/create", "/post/update/123", "/post/delete/123", "/post/images/upload"} {
		if w := b.request("GET", path, nil); w.Code != 403 {
			t.Fatal("non-admin", path, w.Code)
		}
	}
	for _, path := range []string{"/post", "/post/update/123", "/post/delete/123", "/post/images/upload"} {
		if w := b.request("POST", path, url.Values{}); w.Code != 403 {
			t.Fatal("non-admin write", path, w.Code)
		}
	}
	b = newBrowser(app, false)
	if err := createAdmin(t.Context(), app.accounts, adminEmail, adminPassword); err != nil {
		t.Fatal(err)
	}
	login(t, b, adminEmail)
	w := b.request("POST", "/post", url.Values{"title": {" "}, "markdown": {""}})
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Enter a title") {
		t.Fatal("invalid form", w.Code, w.Body)
	}
	if w := b.request("POST", "/post", url.Values{"title": {"large"}, "markdown": {strings.Repeat("x", blog.MaxContentBytes+1)}}); w.Code != 413 {
		t.Fatal("oversized field", w.Code)
	}
	for _, path := range []string{"/posts?page=0", "/posts?page=-1", "/posts?page=abc", "/posts?page=99999999"} {
		if w := b.request("GET", path, nil); w.Code != 400 {
			t.Fatal("invalid pagination", path, w.Code)
		}
	}
	for _, path := range []string{"/missing", "/images/not-an-id", "/blog-assets/nope", "/test-email"} {
		if w := b.request("GET", path, nil); w.Code != 404 {
			t.Fatal(path, w.Code)
		}
	}
	r := httptest.NewRequest("POST", "http://blog.test/post", strings.NewReader("title=bad"))
	r.Header.Set("Origin", "https://attacker.test")
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range b.jar.Cookies(r.URL) {
		r.AddCookie(c)
	}
	w = httptest.NewRecorder()
	b.handler.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin write", w.Code)
	}
	w = b.request("GET", "/auth/register/email", nil)
	if w.Code == 200 && strings.Contains(w.Body.String(), `id="register-form"`) {
		t.Fatal("registration enabled")
	}
}

func upload(b *browser, data []byte) *httptest.ResponseRecorder {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "../../image.svg")
	_, _ = part.Write(data)
	_ = writer.Close()
	return b.raw("POST", "/post/images/upload", writer.FormDataContentType(), &body)
}

func TestImageUploadAndServing(t *testing.T) {
	app := startTest(t, testSettings(t))
	b := newBrowser(app, false)
	if err := createAdmin(t.Context(), app.accounts, adminEmail, adminPassword); err != nil {
		t.Fatal(err)
	}
	login(t, b, adminEmail)
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	w := upload(b, pngData.Bytes())
	match := regexp.MustCompile(`/images/([a-f0-9-]+)`).FindStringSubmatch(w.Body.String())
	if w.Code != 200 || len(match) != 2 {
		t.Fatal("upload", w.Code, w.Body)
	}
	path := match[0]
	anonymous := newBrowser(app, false)
	w = anonymous.request("GET", path, nil)
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), pngData.Bytes()) || w.Header().Get("Content-Type") != "image/png" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("image", w.Code, w.Header())
	}
	if w := anonymous.request("HEAD", path, nil); w.Code != 200 || w.Body.Len() != 0 {
		t.Fatal("HEAD", w.Code, w.Body.Len())
	}
	for _, data := range [][]byte{[]byte("<svg onload='alert(1)'></svg>"), pngData.Bytes()[:30], {}, bytes.Repeat([]byte("x"), blog.MaxImageBytes+1)} {
		w := upload(b, data)
		if w.Code != 400 && w.Code != 413 {
			t.Fatal("invalid upload", w.Code, w.Body)
		}
	}
	for _, format := range []string{"jpeg", "gif"} {
		var data bytes.Buffer
		img := image.NewRGBA(image.Rect(0, 0, 2, 2))
		var err error
		if format == "jpeg" {
			err = jpeg.Encode(&data, img, nil)
		} else {
			err = gif.Encode(&data, img, nil)
		}
		if err != nil {
			t.Fatal(err)
		}
		w := upload(b, data.Bytes())
		match := regexp.MustCompile(`/images/([a-f0-9-]+)`).FindStringSubmatch(w.Body.String())
		if w.Code != 200 || len(match) != 2 {
			t.Fatal(format, "upload", w.Code, w.Body)
		}
		if w := anonymous.request("GET", match[0], nil); w.Header().Get("Content-Type") != "image/"+format || !bytes.Equal(w.Body.Bytes(), data.Bytes()) {
			t.Fatal(format, "serving", w.Code)
		}
	}
	oversized := bytes.Clone(pngData.Bytes())
	binary.BigEndian.PutUint32(oversized[16:20], 5000)
	binary.BigEndian.PutUint32(oversized[20:24], 5000)
	binary.BigEndian.PutUint32(oversized[29:33], crc32.ChecksumIEEE(oversized[12:29]))
	if w := upload(b, oversized); w.Code != 400 || !strings.Contains(w.Body.String(), "16 million pixels") {
		t.Fatal("pixel bound", w.Code, w.Body)
	}
	for _, secondField := range []string{"file", "extra"} {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		for _, name := range []string{"file", secondField} {
			part, _ := writer.CreateFormFile(name, "image.png")
			_, _ = part.Write(pngData.Bytes())
		}
		_ = writer.Close()
		w := b.raw("POST", "/post/images/upload", writer.FormDataContentType(), &body)
		if w.Code != 400 {
			t.Fatal("multiple image upload accepted", w.Code)
		}
	}
}

func TestRestartAndStartupValidation(t *testing.T) {
	s := testSettings(t)
	app := startTest(t, s)
	b := newBrowser(app, false)
	if err := createAdmin(t.Context(), app.accounts, adminEmail, adminPassword); err != nil {
		t.Fatal(err)
	}
	login(t, b, adminEmail)
	assertRedirect(t, b.request("POST", "/post", url.Values{"title": {"Persistent"}, "markdown": {"Saved"}}), "/post/s/persistent", false)
	if err := app.Shutdown(); err != nil {
		t.Fatal(err)
	}
	second := startTest(t, s)
	b = newBrowser(second, false)
	w := b.request("GET", "/post/s/persistent", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Saved") {
		t.Fatal("restart", w.Code, w.Body)
	}
	login(t, b, adminEmail)
	if err := createAdmin(t.Context(), second.accounts, strings.ToUpper(adminEmail), adminPassword); !errors.Is(err, user.ErrEmailAlreadyExists) {
		t.Fatal("reprovision", err)
	}
	for _, change := range []func(*settings){func(s *settings) { s.authKey = "short" }, func(s *settings) { s.sessionKey = s.authKey }, func(s *settings) { s.adminEmail = "bad" }, func(s *settings) { s.publicOrigin = "https://host/path" }} {
		invalid := testSettings(t)
		change(&invalid)
		if app, err := newApplication(t.Context(), invalid); err == nil {
			_ = app.Shutdown()
			t.Fatal("invalid configuration accepted")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if app, err := newApplication(ctx, testSettings(t)); err == nil {
		_ = app.Shutdown()
		t.Fatal("canceled startup accepted")
	}
}

func TestOriginalBlogAssets(t *testing.T) {
	b := newBrowser(startTest(t, testSettings(t)), false)
	for _, file := range []string{
		"blog.css", "forms.css", "source-sans-3.ttf", "source-sans-3-italic.ttf",
		"trent-nix-small.jpg", "site-bg.jpg", "site-bg-gray-faded.jpg",
		"about.png", "projects.png", "linkedin.png", "rss.png", "favicon-32x32.png",
	} {
		w := b.request("GET", "/blog-assets/"+file, nil)
		if w.Code != 200 || w.Body.Len() == 0 {
			t.Fatal(file, w.Code)
		}
	}
	w := b.request("GET", "/", nil)
	for _, want := range []string{`class="header-info"`, `/blog-assets/trent-nix-small.jpg`, `id="posts-list"`, `id="content-menu"`, `id="footer"`} {
		if !strings.Contains(w.Body.String(), want) {
			t.Fatal("original layout missing", want)
		}
	}
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "font-src 'self'") {
		t.Fatal("local fonts blocked by CSP")
	}
}
