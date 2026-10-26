package blog

import (
	"context"
	"crypto/tls"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/trentnix/hyperserver/pkg/services/session"
)

// TestFeedCacheConcurrentBuild checks both coalescing and invalidation during a build.
func TestFeedCacheConcurrentBuild(t *testing.T) {
	for _, invalidate := range []bool{false, true} {
		t.Run(fmt.Sprintf("invalidate=%v", invalidate), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var cache feedCache
				var calls atomic.Int32
				release := make(chan struct{})
				build := func() ([]byte, error) {
					if calls.Add(1) == 1 {
						<-release
						return []byte("old"), nil
					}
					return []byte("new"), nil
				}
				results := make(chan *feedDocument, 20)
				for range cap(results) {
					go func() {
						document, err := cache.load(t.Context(), "https://blog.example/", build)
						if err != nil {
							t.Error(err)
						}
						results <- document
					}()
				}
				synctest.Wait()
				if calls.Load() != 1 {
					t.Fatal("concurrent requests started multiple builds")
				}
				want, wantCalls := "old", int32(1)
				if invalidate {
					cache.invalidate()
					want, wantCalls = "new", 2
				}
				close(release)
				synctest.Wait()
				for range cap(results) {
					document := <-results
					if document == nil || string(document.data) != want || document != cache.value {
						t.Fatalf("request did not share the current document: %v", document)
					}
				}
				if calls.Load() != wantCalls {
					t.Fatalf("builds=%d, want %d", calls.Load(), wantCalls)
				}
			})
		})
	}
}

// TestFeedCacheWaitCancellation keeps a waiting reader from cancelling another reader's build.
func TestFeedCacheWaitCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var cache feedCache
		release := make(chan struct{})
		go func() {
			_, err := cache.load(t.Context(), "origin", func() ([]byte, error) {
				<-release
				return []byte("feed"), nil
			})
			if err != nil {
				t.Error(err)
			}
		}()
		synctest.Wait()
		ctx, cancel := context.WithCancel(t.Context())
		result := make(chan error, 1)
		go func() {
			_, err := cache.load(ctx, "origin", func() ([]byte, error) {
				t.Error("waiting request started a build")
				return nil, nil
			})
			result <- err
		}()
		synctest.Wait()
		cancel()
		synctest.Wait()
		if err := <-result; !errors.Is(err, context.Canceled) {
			t.Fatalf("waiting request: %v", err)
		}
		close(release)
		synctest.Wait()
		if cache.value == nil || string(cache.value.data) != "feed" {
			t.Fatal("waiting request disturbed the build")
		}
		if _, err := cache.load(ctx, "origin", nil); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled cache hit: %v", err)
		}
	})
}

// TestFeedCacheFailedBuildsRetry ensures errors and cancelled work never become cached content.
func TestFeedCacheFailedBuildsRetry(t *testing.T) {
	for _, cancelBuild := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%v", cancelBuild), func(t *testing.T) {
			var cache feedCache
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			failure := errors.New("storage failed")
			_, err := cache.load(ctx, "origin", func() ([]byte, error) {
				if cancelBuild {
					cancel()
					return []byte("discard"), nil
				}
				return nil, failure
			})
			if err == nil || cache.value != nil || cache.loading != nil {
				t.Fatalf("failed build retained state: %v", err)
			}
			document, err := cache.load(t.Context(), "origin", func() ([]byte, error) { return []byte("retry"), nil })
			if err != nil || string(document.data) != "retry" {
				t.Fatalf("retry: %v, %v", document, err)
			}
		})
	}
}

// TestFeedCachePanicReleasesBuild leaves panic recovery to net/http without wedging the cache.
func TestFeedCachePanicReleasesBuild(t *testing.T) {
	var cache feedCache
	func() {
		defer func() {
			if recover() != "render failed" {
				t.Error("cache swallowed or changed the rendering panic")
			}
		}()
		cache.load(t.Context(), "origin", func() ([]byte, error) { panic("render failed") })
	}()
	if cache.value != nil || cache.loading != nil {
		t.Fatal("panic left cached data or an occupied build slot")
	}
	document, err := cache.load(t.Context(), "origin", func() ([]byte, error) { return []byte("retry"), nil })
	if err != nil || string(document.data) != "retry" {
		t.Fatalf("retry after panic: %v, %v", document, err)
	}
}

// feedRepository counts or blocks storage reads without implementing unrelated operations.
type feedRepository struct {
	repository
	list func(context.Context) ([]Post, error)
}

func (s feedRepository) List(ctx context.Context) ([]Post, error) { return s.list(ctx) }

// TestRSSConditionalRequests checks standard HTTP validators without rebuilding XML.
func TestRSSConditionalRequests(t *testing.T) {
	h := newHarness(t)
	calls := 0
	m := &module{app: h.app.ApplicationServer, profile: h.blog.profile, posts: feedRepository{list: func(context.Context) ([]Post, error) {
		calls++
		return []Post{{ID: "one", Slug: "hello", Title: "Hello", Markdown: "Body"}}, nil
	}}}
	request := func(method, tag string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://untrusted.example/rss", nil)
		r.Header.Set("If-None-Match", tag)
		r.Header.Set("X-Forwarded-Host", "untrusted.example")
		w := httptest.NewRecorder()
		m.rss(w, r)
		return w
	}
	first := request(http.MethodGet, "")
	requireStatus(t, first, http.StatusOK)
	etag := first.Header().Get("ETag")
	if etag == "" || first.Header().Get("Cache-Control") != "no-cache" || first.Header().Get("Content-Type") != "application/rss+xml; charset=utf-8" {
		t.Fatalf("feed headers: %v", first.Header())
	}
	if !strings.Contains(first.Body.String(), "https://blog.example/post/s/hello") || strings.Contains(first.Body.String(), "untrusted.example") {
		t.Fatal("feed URLs did not use configured origin")
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for _, tag := range []string{etag, "W/" + etag, `"old", ` + etag, "*"} {
			w := request(method, tag)
			requireStatus(t, w, http.StatusNotModified)
			if w.Body.Len() != 0 || w.Header().Get("ETag") != etag || w.Header().Get("Cache-Control") != "no-cache" {
				t.Fatalf("invalid conditional response: %v %s", w.Header(), w.Body.String())
			}
		}
		w := request(method, `"different"`)
		requireStatus(t, w, http.StatusOK)
		if method == http.MethodHead && (w.Body.Len() != 0 || w.Header().Get("Content-Length") != fmt.Sprint(first.Body.Len())) {
			t.Fatal("HEAD must describe the feed without a body")
		}
		if method == http.MethodGet && w.Body.String() != first.Body.String() {
			t.Fatal("cached XML changed")
		}
	}
	if calls != 1 {
		t.Fatalf("feed read storage %d times", calls)
	}
}

// TestRSSOriginIsolation also covers the HTTP/TLS fallback when publicOrigin is unset.
func TestRSSOriginIsolation(t *testing.T) {
	h := newHarness(t)
	h.app.Config.HTTP.PublicOrigin = ""
	calls := 0
	store := feedRepository{list: func(context.Context) ([]Post, error) { calls++; return nil, nil }}
	m := &module{app: h.app.ApplicationServer, profile: h.blog.profile, posts: store}
	for _, secure := range []bool{false, true, true, false} {
		r := httptest.NewRequest(http.MethodGet, "http://untrusted.example/rss", nil)
		scheme := "http"
		if secure {
			r.TLS, scheme = &tls.ConnectionState{}, "https"
		}
		w := httptest.NewRecorder()
		m.rss(w, r)
		if !strings.Contains(w.Body.String(), scheme+"://127.0.0.1:8086/") {
			t.Fatalf("wrong origin: %s", w.Body.String())
		}
	}
	if calls != 3 {
		t.Fatalf("origin cache builds=%d, want 3", calls)
	}
	other := &module{app: h.app.ApplicationServer, profile: h.blog.profile, posts: store}
	other.rss(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/rss", nil))
	if calls != 4 {
		t.Fatal("module instances shared cached content")
	}
}

// TestRSSStorageFailureDoesNotCache allows the next request to recover.
func TestRSSStorageFailureDoesNotCache(t *testing.T) {
	h := newHarness(t)
	calls := 0
	m := &module{app: h.app.ApplicationServer, profile: h.blog.profile, posts: feedRepository{list: func(context.Context) ([]Post, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("private storage failure")
		}
		return nil, nil
	}}}
	for _, status := range []int{http.StatusInternalServerError, http.StatusOK, http.StatusOK} {
		r := session.AddSessionManagerToRequestContext(httptest.NewRequest(http.MethodGet, "/rss", nil), h.app.Sessions)
		w := httptest.NewRecorder()
		m.rss(w, r)
		requireStatus(t, w, status)
		if strings.Contains(w.Body.String(), "private storage") {
			t.Fatal("feed exposed storage details")
		}
		if status == http.StatusInternalServerError && (w.Header().Get("ETag") != "" || w.Header().Get("Cache-Control") != "no-store") {
			t.Fatal("error response advertised cached feed metadata")
		}
	}
	if calls != 2 {
		t.Fatalf("storage calls=%d, want 2", calls)
	}
}

// TestRSSPostChanges exercises invalidation through the application's publishing routes.
func TestRSSPostChanges(t *testing.T) {
	h := newHarnessWithRecentPostCount(t, 1)
	h.register(t)
	h.verify(t)
	h.login(t)
	read := func(count int, previous string) (feed, string) {
		t.Helper()
		w := h.request(http.MethodGet, "/rss", nil, false)
		requireStatus(t, w, http.StatusOK)
		var result feed
		if err := xml.Unmarshal(w.Body.Bytes(), &result); err != nil || len(result.Channel.Items) != count {
			t.Fatalf("feed items: %v, %s", err, w.Body.String())
		}
		etag := w.Header().Get("ETag")
		if etag == "" || etag == previous {
			t.Fatal("committed change did not update ETag")
		}
		return result, etag
	}
	_, emptyTag := read(0, "")
	values := url.Values{"slug": {"first"}, "title": {"First"}, "content": {"![picture](/images/one)"}}
	requireStatus(t, h.request(http.MethodPost, "/post", values, false), http.StatusSeeOther)
	h.publish(t, values.Get("slug"))
	first, firstTag := read(1, emptyTag)
	if !strings.Contains(first.Channel.Items[0].Content, `src="https://blog.example/images/one"`) {
		t.Fatal("feed image is not absolute")
	}
	id := strings.TrimPrefix(first.Channel.Items[0].GUID, "https://blog.example/post/id/")
	values.Set("slug", "second")
	values.Set("title", "Second")
	requireStatus(t, h.request(http.MethodPost, "/post", values, false), http.StatusSeeOther)
	h.publish(t, values.Get("slug"))
	_, secondTag := read(2, firstTag)
	values.Set("slug", "first")
	values.Set("title", "Edited")
	values.Set("published", "on")
	requireStatus(t, h.request(http.MethodPost, "/post/"+id, values, false), http.StatusSeeOther)
	edited, editedTag := read(2, secondTag)
	if edited.Channel.Items[1].Title != "Edited" || edited.Channel.Items[1].GUID != first.Channel.Items[0].GUID {
		t.Fatal("edit did not preserve stable feed identity")
	}
	requireStatus(t, h.request(http.MethodPost, "/post", values, false), http.StatusConflict)
	requireStatus(t, h.request(http.MethodPost, "/post/delete/missing", nil, false), http.StatusNotFound)
	values.Set("title", "")
	requireStatus(t, h.request(http.MethodPost, "/post/"+id, values, false), http.StatusUnprocessableEntity)
	if tag := h.request(http.MethodGet, "/rss", nil, false).Header().Get("ETag"); tag != editedTag {
		t.Fatal("rejected write changed the feed")
	}
	requireStatus(t, h.request(http.MethodPost, "/post/delete/"+id, nil, false), http.StatusSeeOther)
	read(1, editedTag)
}

// TestFeedHTML keeps feed references equivalent to links on the post page.
func TestFeedHTML(t *testing.T) {
	base, _ := url.Parse("https://blog.example/post/s/hello")
	for _, tc := range []struct{ markdown, want string }{
		{`![photo](/images/one)`, `src="https://blog.example/images/one"`},
		{`[root](/about)`, `href="https://blog.example/about"`},
		{`[relative](other?x=1&y=2#part)`, `href="https://blog.example/post/s/other?x=1&amp;y=2#part"`},
		{`[fragment](#part)`, `href="https://blog.example/post/s/hello#part"`},
		{`[query](?a=1&amp;b=2)`, `href="https://blog.example/post/s/hello?a=1&amp;b=2"`},
		{`![external](//cdn.example/a.png)`, `src="https://cdn.example/a.png"`},
		{`[absolute](https://other.example/a)`, `href="https://other.example/a"`},
		{`[mail](mailto:reader@example.test)`, `href="mailto:reader@example.test"`},
		{`[escaped](/a%20b?q=%22x%22)`, `href="https://blog.example/a%20b?q=%22x%22"`},
		{`[bad](javascript:alert%281%29)`, `href=""`},
		{`![bad](javascript:alert%281%29)`, `src=""`},
		{`[bad](jav&#x61;script:alert%281%29)`, `href=""`},
		{"`[code](/about)`", `<code>[code](/about)</code>`},
	} {
		t.Run(tc.markdown, func(t *testing.T) {
			got, err := feedHTML(tc.markdown, base)
			if err != nil || !strings.Contains(got, tc.want) {
				t.Fatalf("feed HTML=%q, error=%v, want %q", got, err, tc.want)
			}
		})
	}
	got, err := feedHTML(`<script>alert(1)</script>`, base)
	if err != nil || strings.Contains(got, "<script>") {
		t.Fatalf("raw HTML was enabled: %q, %v", got, err)
	}
}
