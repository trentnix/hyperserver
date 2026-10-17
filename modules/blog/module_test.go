package blog

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

type stubStorage struct {
	posts             []Post
	image             Image
	readErr, writeErr error
	writes            int
}

func (s *stubStorage) List(_ context.Context, offset, limit int) ([]Post, error) {
	if s.readErr != nil {
		return nil, s.readErr
	}
	if offset >= len(s.posts) {
		return nil, nil
	}
	return s.posts[offset:min(offset+limit, len(s.posts))], nil
}
func (s *stubStorage) GetByID(context.Context, string) (Post, error) {
	if s.readErr != nil {
		return Post{}, s.readErr
	}
	if len(s.posts) == 0 {
		return Post{}, ErrNotFound
	}
	return s.posts[0], nil
}
func (s *stubStorage) GetBySlug(ctx context.Context, id string) (Post, error) {
	return s.GetByID(ctx, id)
}
func (s *stubStorage) Create(context.Context, Post) error          { s.writes++; return s.writeErr }
func (s *stubStorage) Update(context.Context, Post) error          { s.writes++; return s.writeErr }
func (s *stubStorage) Delete(context.Context, string, int64) error { s.writes++; return s.writeErr }
func (s *stubStorage) Save(context.Context, Image) error           { s.writes++; return s.writeErr }
func (s *stubStorage) Get(context.Context, string) (Image, error)  { return s.image, s.readErr }

func moduleOptions(store *stubStorage) Options {
	return Options{Posts: store, Images: store, Protect: func(h http.Handler) http.Handler { return h }, AdminEmails: []string{"admin@example.com"}, Title: "Test", PublicOrigin: "https://blog.example"}
}

func testModule(t *testing.T, store *stubStorage) (*module, http.Handler) {
	t.Helper()
	app, err := server.NewApplicationServer(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ConfigureRendering(app.ContentManager); err != nil {
		t.Fatal(err)
	}
	app.ContentManager.BuildLayoutData = func(*http.Request) any { return LayoutData{Title: "Test"} }
	m := Module(moduleOptions(store)).New().(*module)
	if err := m.Init(t.Context(), app); err != nil {
		t.Fatal(err)
	}
	if err := m.Routes(routing.NewRoutes(app.Web)); err != nil {
		t.Fatal(err)
	}
	return m, app.Web
}

func moduleRequest(handler http.Handler, method, path string, values url.Values, account *user.User) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(values.Encode()))
	if values != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if account != nil {
		r = user.AddUserToRequestContext(r, account)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestModuleValidationAndIsolation(t *testing.T) {
	store := &stubStorage{}
	app, _ := server.NewApplicationServer(config.Config{})
	for name, change := range map[string]func(*Options){
		"nil posts":        func(o *Options) { o.Posts = nil },
		"typed nil posts":  func(o *Options) { var s *stubStorage; o.Posts = s },
		"nil images":       func(o *Options) { o.Images = nil },
		"typed nil images": func(o *Options) { var s *stubStorage; o.Images = s },
		"no guard":         func(o *Options) { o.Protect = nil },
		"no admin":         func(o *Options) { o.AdminEmails = nil },
		"invalid admin":    func(o *Options) { o.AdminEmails = []string{"invalid"} },
		"origin":           func(o *Options) { o.PublicOrigin = "//attacker.example" },
		"origin path":      func(o *Options) { o.PublicOrigin = "https://blog.example/path" },
		"empty origin":     func(o *Options) { o.PublicOrigin = "" },
		"origin query":     func(o *Options) { o.PublicOrigin = "https://blog.example?" },
		"origin fragment":  func(o *Options) { o.PublicOrigin = "https://blog.example#" },
		"origin hostname":  func(o *Options) { o.PublicOrigin = "https://:443" },
		"origin port":      func(o *Options) { o.PublicOrigin = "https://blog.example:65536" },
		"origin IPv6":      func(o *Options) { o.PublicOrigin = "https://[not-an-ip]" },
		"title":            func(o *Options) { o.Title = " " },
	} {
		t.Run(name, func(t *testing.T) {
			o := moduleOptions(store)
			change(&o)
			if err := Module(o).New().(*module).Init(t.Context(), app); err == nil {
				t.Fatal("invalid module initialized")
			}
		})
	}
	o := moduleOptions(store)
	descriptor := Module(o)
	o.AdminEmails[0] = "other@example.com"
	first, second := descriptor.New().(*module), descriptor.New().(*module)
	for _, m := range []*module{first, second} {
		if err := m.Init(t.Context(), app); err != nil {
			t.Fatal(err)
		}
	}
	delete(first.admins, user.EmailKey("admin@example.com"))
	if !second.admins[user.EmailKey("admin@example.com")] {
		t.Fatal("module instances share mutable state or caller's allowlist")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := second.Init(ctx, app); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestModuleStorageFailures(t *testing.T) {
	store := &stubStorage{readErr: errors.New("private-storage-detail")}
	_, handler := testModule(t, store)
	for _, path := range []string{"/", "/posts", "/rss", "/post/s/post", "/post/id/post", "/images/" + uuid.NewString()} {
		w := moduleRequest(handler, "GET", path, nil, nil)
		if w.Code != 503 || strings.Contains(w.Body.String(), "private-storage-detail") {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
	}
	store.readErr = nil
	store.writeErr = errors.New("private-storage-detail")
	p := Post{ID: uuid.NewString(), Title: "Post", Slug: "post", Markdown: "Content", PublishedAt: time.Now(), Version: 1}
	store.posts = []Post{p}
	admin := &user.User{Email: "admin@example.com"}
	values := url.Values{"title": {"Post"}, "markdown": {"Content"}, "version": {"1"}}
	for _, path := range []string{"/post", "/post/update/" + p.ID, "/post/delete/" + p.ID} {
		w := moduleRequest(handler, "POST", path, values, admin)
		if w.Code != 503 || w.Header().Get("Location") != "" || strings.Contains(w.Body.String(), "private-storage-detail") {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
	}
	var body, imageBytes bytes.Buffer
	if err := png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "a.png")
	_, _ = part.Write(imageBytes.Bytes())
	_ = writer.Close()
	r := httptest.NewRequest("POST", "/post/images/upload", &body)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	r = user.AddUserToRequestContext(r, admin)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 503 || strings.Contains(w.Body.String(), "Image uploaded") {
		t.Fatal("false upload success", w.Code, w.Body)
	}
}

func TestModulePublicOrigin(t *testing.T) {
	for _, origin := range []string{"https://blog.example", "https://blog.example/", "http://[::1]:8085/"} {
		t.Run(origin, func(t *testing.T) {
			store := &stubStorage{posts: []Post{{ID: uuid.NewString(), Slug: "post", Title: "Post", Markdown: "Content", PublishedAt: time.Now(), Version: 1}}}
			app, err := server.NewApplicationServer(config.Config{})
			if err != nil {
				t.Fatal(err)
			}
			options := moduleOptions(store)
			options.PublicOrigin = origin
			m := Module(options).New().(*module)
			if err := m.Init(t.Context(), app); err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			m.rss(w, httptest.NewRequest("GET", "/rss", nil))
			want := "<link>" + strings.TrimSuffix(origin, "/") + "/post/s/post</link>"
			if w.Code != 200 || !strings.Contains(w.Body.String(), want) {
				t.Fatalf("RSS origin: %d %s", w.Code, w.Body)
			}
		})
	}
}

func TestEditorLimitsAndVersion(t *testing.T) {
	p := Post{ID: uuid.NewString(), Slug: "post", Title: "Post", Markdown: "Original", PublishedAt: time.Now(), Version: 2}
	store := &stubStorage{posts: []Post{p}}
	_, handler := testModule(t, store)
	admin := &user.User{Email: "ADMIN@example.com"}
	values := url.Values{"title": {"Full size"}, "markdown": {strings.Repeat("&", MaxContentBytes)}}
	w := moduleRequest(handler, "POST", "/post", values, admin)
	if w.Code != 303 || store.writes != 1 {
		t.Fatal("legal encoded form rejected", w.Code, w.Body)
	}
	values.Set("markdown", "Submitted text")
	values.Set("version", "invalid")
	w = moduleRequest(handler, "POST", "/post/update/"+p.ID, values, admin)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `name="version" value="0"`) || !strings.Contains(w.Body.String(), "Submitted text") || store.writes != 1 {
		t.Fatal("invalid version replaced with current version", w.Code, w.Body)
	}
	admin.VerificationRequired = true
	if w := moduleRequest(handler, "POST", "/post", values, admin); w.Code != 403 || store.writes != 1 {
		t.Fatal("unverified editor allowed")
	}
}

func TestEditorRejectsInvalidUTF8Fields(t *testing.T) {
	for _, fields := range []struct {
		title, summary, markdown string
		invalid                  []string
	}{
		{"\xc3", "\xa9", "Text", []string{"Title", "Summary"}},
		{"Title", "\xc3", "\xa9", []string{"Summary", "Markdown"}},
		{"\xa9", "", "\xc3", []string{"Title", "Markdown"}},
	} {
		f := editForm{Post: Post{Slug: "post", Title: fields.title, Summary: fields.summary, Markdown: fields.markdown}}
		errors := f.Validate()
		for _, field := range fields.invalid {
			if len(errors[field]) == 0 {
				t.Errorf("invalid UTF-8 accepted for %s in %#v", field, f.Post)
			}
		}
	}

	p := Post{ID: uuid.NewString(), Slug: "post", Title: "Post", Markdown: "Original", PublishedAt: time.Now(), Version: 1}
	store := &stubStorage{posts: []Post{p}}
	_, handler := testModule(t, store)
	values := url.Values{"slug": {"post"}, "title": {"\xc3"}, "summary": {"\xa9"}, "markdown": {"Text"}, "version": {"1"}}
	for _, path := range []string{"/post", "/post/update/" + p.ID} {
		w := moduleRequest(handler, "POST", path, values, &user.User{Email: "admin@example.com"})
		if w.Code != 200 || !strings.Contains(w.Body.String(), "valid UTF-8") || store.writes != 0 {
			t.Fatalf("invalid UTF-8 reached storage: %s: %d, writes=%d", path, w.Code, store.writes)
		}
	}

	f := editForm{Post: Post{Slug: "post", Title: "日本語", Summary: "Café", Markdown: "Hello 🌍"}}
	if fields := f.Validate(); len(fields) != 0 {
		t.Fatalf("valid UTF-8 rejected: %v", fields)
	}
}

func TestArchivePaginationAndRSSBounds(t *testing.T) {
	store := &stubStorage{}
	for i := 0; i < MaxListSize+1; i++ {
		store.posts = append(store.posts, Post{ID: uuid.NewString(), Slug: fmt.Sprintf("post-%d", i), Title: fmt.Sprintf("Title %d", i), Markdown: "Content", PublishedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC).Add(-time.Duration(i) * 24 * time.Hour), Version: 1})
	}
	_, handler := testModule(t, store)
	w := moduleRequest(handler, "GET", "/posts", nil, nil)
	if strings.Count(w.Body.String(), `href="/post/s/`) != PageSize || !strings.Contains(w.Body.String(), "/posts?page=2") || !strings.Contains(w.Body.String(), "September 2026") {
		t.Fatal("archive", w.Body)
	}
	w = moduleRequest(handler, "GET", "/posts?page=2", nil, nil)
	if !strings.Contains(w.Body.String(), "/posts?page=1") || strings.Contains(w.Body.String(), `href="/post/s/post-0"`) {
		t.Fatal("archive offset", w.Body)
	}
	w = moduleRequest(handler, "GET", "/rss", nil, nil)
	if strings.Count(w.Body.String(), "<item>") != MaxListSize || !strings.Contains(w.Body.String(), "https://blog.example/post/s/post-0") {
		t.Fatal("RSS bounds or origin", w.Body)
	}
	w = moduleRequest(handler, "HEAD", "/rss", nil, nil)
	if w.Code != 200 || w.Body.Len() != 0 {
		t.Fatal("RSS HEAD", w.Code, w.Body)
	}
}

func TestStoredImageRejections(t *testing.T) {
	store := &stubStorage{}
	_, handler := testModule(t, store)
	for _, img := range []Image{{MediaType: "image/svg+xml", Data: []byte("svg")}, {MediaType: "image/png"}, {MediaType: "image/png", Data: make([]byte, MaxImageBytes+1)}} {
		store.image = img
		if w := moduleRequest(handler, "GET", "/images/"+uuid.NewString(), nil, nil); w.Code != 500 {
			t.Fatal("invalid stored image", w.Code)
		}
	}
}

func TestRecentPostsFragment(t *testing.T) {
	store := &stubStorage{posts: []Post{{ID: uuid.NewString(), Slug: "hello", Title: "Hello <world>", Markdown: "Text", PublishedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Version: 1}}}
	_, handler := testModule(t, store)
	for _, history := range []bool{false, true} {
		r := httptest.NewRequest("GET", "/posts", nil)
		r.Header.Set("HX-Request", "true")
		r.Header.Set("HX-Target", "posts-list")
		if history {
			r.Header.Set("HX-History-Restore-Request", "true")
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		body := w.Body.String()
		if w.Code != 200 || !strings.Contains(body, "Hello &lt;world&gt;") || !strings.Contains(strings.Join(w.Header().Values("Vary"), ","), "HX-Target") {
			t.Fatalf("recent posts = %d %s", w.Code, body)
		}
		if strings.Contains(body, "<!doctype html>") != history {
			t.Fatal("wrong document variant", body)
		}
		if !history && (!strings.Contains(body, `<div class="title">Recent Posts</div>`) || !strings.Contains(body, "October 2026") || strings.Contains(body, "All posts</h1>")) {
			t.Fatal("wrong sidebar content", body)
		}
	}
	store.readErr = errors.New("private failure")
	r := httptest.NewRequest("GET", "/posts", nil)
	r.Header.Set("HX-Request", "true")
	r.Header.Set("HX-Target", "posts-list")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 503 || strings.Contains(w.Body.String(), "private failure") {
		t.Fatal("sidebar storage failure", w.Code, w.Body)
	}
}
