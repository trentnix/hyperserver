package blog

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// TestSlugFromTitle covers normalization, empty results, and the URL length limit.
func TestSlugFromTitle(t *testing.T) {
	for _, test := range []struct{ title, want string }{
		{"Hello World", "hello-world"},
		{"  Go & HTMX: a practical guide!  ", "go-htmx-a-practical-guide"},
		{"Version 2.0 -- Update", "version-2-0-update"},
		{"Tabs\tand\nlines", "tabs-and-lines"},
		{"Hello 世界", "hello"},
		{"İGo Kit", "go-it"},
		{"世界", ""},
		{" !!! ", ""},
		{"", ""},
		{strings.Repeat("A", 240), strings.Repeat("a", 120)},
		{strings.Repeat("a", 119) + " -- end", strings.Repeat("a", 119)},
	} {
		t.Run(test.title, func(t *testing.T) {
			got := slugFromTitle(test.title)
			if got != test.want || got != "" && !validSlug(got) {
				t.Fatalf("slugFromTitle(%q) = %q, want %q", test.title, got, test.want)
			}
		})
	}
}

// TestNumberedSlug leaves room for the suffix without trailing or repeated hyphens.
func TestNumberedSlug(t *testing.T) {
	for _, test := range []struct {
		base   string
		number int
		want   string
	}{
		{"my-post", 2, "my-post-2"},
		{"my-post-2", 3, "my-post-2-3"},
		{strings.Repeat("a", 120), 10, strings.Repeat("a", 117) + "-10"},
		{strings.Repeat("a", 117) + "-bc", 2, strings.Repeat("a", 117) + "-2"},
	} {
		got := numberedSlug(test.base, test.number)
		if got != test.want || !validSlug(got) {
			t.Errorf("numberedSlug(%q, %d) = %q, want %q", test.base, test.number, got, test.want)
		}
	}
}

// TestTitleSlugs checks both editors through creation, conflicts, and edits.
func TestTitleSlugs(t *testing.T) {
	for _, path := range []string{"/post", "/pages"} {
		for _, hx := range []bool{false, true} {
			name := path + map[bool]string{false: "/ordinary", true: "/htmx"}[hx]
			t.Run(name, func(t *testing.T) {
				h := newHarness(t)
				h.register(t)
				h.verify(t)
				h.login(t)
				status, header := http.StatusSeeOther, "Location"
				urlPrefix := "/post/s/"
				if hx {
					status, header = http.StatusOK, "HX-Redirect"
				}
				if path == "/pages" {
					urlPrefix = "/page/"
				}
				fields := url.Values{"title": {"A Useful Guide!"}, "content": {"Body"}, "slug": {"  "}}
				fields.Set("title", strings.Repeat("A", 241))
				invalid := h.request("POST", path, fields, hx)
				requireStatus(t, invalid, http.StatusUnprocessableEntity)
				if !hx && !strings.Contains(invalid.Body.String(), `name="slug" value=""`) {
					t.Fatal("validation failure turned an automatic slug into an explicit slug")
				}
				fields.Set("title", "A Useful Guide!")
				response := h.request("POST", path, fields, hx)
				requireStatus(t, response, status)
				if response.Header().Get(header) != urlPrefix+"a-useful-guide" {
					t.Fatal("missing title-based redirect", response.Header())
				}
				var id string
				if path == "/post" {
					post, err := h.blog.posts.GetBySlug(t.Context(), "a-useful-guide", true)
					if err != nil {
						t.Fatal(err)
					}
					id = post.ID
				} else {
					page, err := h.blog.pageStore.GetPageBySlug(t.Context(), "a-useful-guide", true)
					if err != nil {
						t.Fatal(err)
					}
					id = page.ID
				}
				for _, suffix := range []string{"-2", "-3"} {
					response = h.request("POST", path, fields, hx)
					requireStatus(t, response, status)
					if response.Header().Get(header) != urlPrefix+"a-useful-guide"+suffix {
						t.Fatal("generated slug did not advance to the next available suffix", response.Header())
					}
				}
				fields.Set("slug", "a-useful-guide")
				conflict := h.request("POST", path, fields, hx)
				requireStatus(t, conflict, http.StatusConflict)
				if !strings.Contains(conflict.Body.String(), "This URL slug is already in use.") || !strings.Contains(conflict.Body.String(), "leave it blank to generate one from the title") {
					t.Fatal("slug conflict must explain how to resolve it")
				}
				fields.Set("title", "A Changed Title")
				fields.Set("slug", "a-useful-guide")
				response = h.submitEditor(t, path+"/"+id, fields, hx)
				requireStatus(t, response, status)
				if response.Header().Get(header) != urlPrefix+"a-useful-guide" {
					t.Fatal("editing the title changed an existing slug")
				}
				fields.Del("slug")
				response = h.submitEditor(t, path+"/"+id, fields, hx)
				requireStatus(t, response, status)
				if response.Header().Get(header) != urlPrefix+"a-changed-title" {
					t.Fatal("clearing the slug did not regenerate it from the title")
				}
				fields.Set("title", "世界")
				requireStatus(t, h.request("POST", path, fields, hx), http.StatusUnprocessableEntity)
				fields.Set("slug", "custom-slug")
				response = h.request("POST", path, fields, hx)
				requireStatus(t, response, status)
				if response.Header().Get(header) != urlPrefix+"custom-slug" {
					t.Fatal("explicit slug was not preserved")
				}
				fields.Set("slug", "INVALID slug")
				requireStatus(t, h.request("POST", path, fields, hx), http.StatusUnprocessableEntity)
			})
		}
	}
}

// TestGeneratedSlugConcurrency exercises uniqueness through the real HTTP and storage paths.
func TestGeneratedSlugConcurrency(t *testing.T) {
	for _, path := range []string{"/post", "/pages"} {
		t.Run(path, func(t *testing.T) {
			h := newHarness(t)
			h.register(t)
			h.verify(t)
			h.login(t)
			const count = 6
			start := make(chan struct{})
			results := make(chan *httptest.ResponseRecorder, count)
			for range count {
				go func() {
					<-start
					fields := url.Values{"title": {strings.Repeat("A", 120)}, "content": {"Body"}}
					r := httptest.NewRequest("POST", "https://blog.example"+path, strings.NewReader(fields.Encode()))
					r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					for _, cookie := range h.cookies {
						r.AddCookie(cookie)
					}
					w := httptest.NewRecorder()
					h.app.Handler.ServeHTTP(w, r)
					results <- w
				}()
			}
			close(start)
			locations := make(map[string]bool)
			for range count {
				response := <-results
				if response.Code != http.StatusSeeOther {
					t.Errorf("concurrent save returned %d: %s", response.Code, response.Body.String())
				}
				location := response.Header().Get("Location")
				if locations[location] {
					t.Errorf("concurrent saves reused %q", location)
				}
				locations[location] = true
			}
			prefix := "/post/s/"
			if path == "/pages" {
				prefix = "/page/"
			}
			for number := 1; number <= count; number++ {
				slug := strings.Repeat("a", 120)
				if number > 1 {
					suffix := "-" + strconv.Itoa(number)
					slug = slug[:120-len(suffix)] + suffix
				}
				if !locations[prefix+slug] {
					t.Errorf("missing generated URL %s", prefix+slug)
				}
				requireStatus(t, h.request("GET", prefix+slug, nil, false), http.StatusOK)
			}
		})
	}
}

// slugFailureStore injects failures after a generated slug collides.
type slugFailureStore struct {
	repository
	pageRepository
	save func(string) error
}

func (s slugFailureStore) Save(_ context.Context, p Post) (Post, error) {
	return p, s.save(p.Slug)
}

func (s slugFailureStore) SavePage(_ context.Context, p Page) (Page, error) {
	return p, s.save(p.Slug)
}

// TestGeneratedSlugFailure stops retries on storage errors or request cancellation.
func TestGeneratedSlugFailure(t *testing.T) {
	for _, path := range []string{"/post", "/pages"} {
		for _, canceled := range []bool{false, true} {
			t.Run(path+"/canceled="+strconv.FormatBool(canceled), func(t *testing.T) {
				h := newHarness(t)
				h.register(t)
				h.verify(t)
				h.login(t)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				calls := 0
				store := slugFailureStore{repository: h.blog.posts, pageRepository: h.blog.pageStore}
				store.save = func(slug string) error {
					calls++
					if calls > 1 {
						if slug != "same-title-2" {
							t.Errorf("retry used %q", slug)
						}
						return errors.New("storage unavailable")
					}
					if canceled {
						cancel()
					}
					if path == "/pages" {
						return errPageSlugTaken
					}
					return errSlugTaken
				}
				h.blog.posts, h.blog.pageStore = store, store
				fields := url.Values{"title": {"Same Title"}, "content": {"Body"}}
				r := httptest.NewRequest("POST", "https://blog.example"+path, strings.NewReader(fields.Encode())).WithContext(ctx)
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				r.Header.Set("HX-Request", "true")
				for _, cookie := range h.cookies {
					r.AddCookie(cookie)
				}
				w := httptest.NewRecorder()
				h.app.Handler.ServeHTTP(w, r)
				wantCalls := 2
				if canceled {
					wantCalls = 1
				}
				if calls != wantCalls || w.Code < 400 || w.Header().Get("HX-Trigger") != "" {
					t.Fatalf("failed save: calls=%d status=%d headers=%v", calls, w.Code, w.Header())
				}
			})
		}
	}
}
