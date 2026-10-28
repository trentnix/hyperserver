package blog

import (
	"context"
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
	"time"

	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/services/session"
)

// TestRecentCacheConcurrentRead checks coalescing and invalidation during storage reads.
func TestRecentCacheConcurrentRead(t *testing.T) {
	for _, invalidate := range []bool{false, true} {
		t.Run(fmt.Sprintf("invalidate=%v", invalidate), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var cache cachedValue[int, []ArchiveEntry]
				var calls atomic.Int32
				release := make(chan struct{})
				read := func() ([]ArchiveEntry, error) {
					if calls.Add(1) == 1 {
						<-release
						return []ArchiveEntry{{Title: "Old"}}, nil
					}
					return []ArchiveEntry{{Title: "New"}}, nil
				}
				results := make(chan []ArchiveEntry, 20)
				for range cap(results) {
					go func() {
						entries, err := cache.load(t.Context(), 1, read)
						if err != nil {
							t.Error(err)
						}
						results <- entries
					}()
				}
				synctest.Wait()
				if calls.Load() != 1 {
					t.Fatal("concurrent requests started multiple reads")
				}
				want, wantCalls := "Old", int32(1)
				if invalidate {
					cache.invalidate()
					want, wantCalls = "New", 2
				}
				close(release)
				synctest.Wait()
				for range cap(results) {
					entries := <-results
					if len(entries) != 1 || entries[0].Title != want {
						t.Fatalf("unexpected entries: %+v", entries)
					}
				}
				if calls.Load() != wantCalls {
					t.Fatalf("reads=%d, want %d", calls.Load(), wantCalls)
				}
			})
		})
	}
}

// TestRecentCacheLeaderFailure checks that queued readers recover after every failure exit.
func TestRecentCacheLeaderFailure(t *testing.T) {
	for _, failure := range []string{"error", "cancel", "panic"} {
		t.Run(failure, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var cache cachedValue[int, []ArchiveEntry]
				release := make(chan struct{})
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				go func() {
					defer func() {
						got := recover()
						if failure == "panic" && got != "read failed" || failure != "panic" && got != nil {
							t.Errorf("unexpected panic: %v", got)
						}
					}()
					_, err := cache.load(ctx, 1, func() ([]ArchiveEntry, error) {
						<-release
						switch failure {
						case "panic":
							panic("read failed")
						case "cancel":
							cancel()
							return []ArchiveEntry{{Title: "Discard"}}, nil
						default:
							return nil, errors.New("storage failed")
						}
					})
					if err == nil || failure == "cancel" && !errors.Is(err, context.Canceled) {
						t.Errorf("unexpected read error: %v", err)
					}
				}()
				synctest.Wait()
				go func() {
					entries, err := cache.load(t.Context(), 1, func() ([]ArchiveEntry, error) {
						return []ArchiveEntry{{Title: "Retry"}}, nil
					})
					if err != nil || len(entries) != 1 || entries[0].Title != "Retry" {
						t.Errorf("waiting reader did not recover: %+v, %v", entries, err)
					}
				}()
				synctest.Wait()
				close(release)
				synctest.Wait()
				if !cache.ready || cache.loading != nil {
					t.Fatal("retry left incorrect cache state")
				}
			})
		})
	}
}

// TestRecentCacheWaitCancellation preserves the active read and caches an empty result.
func TestRecentCacheWaitCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var cache cachedValue[int, []ArchiveEntry]
		release := make(chan struct{})
		go func() {
			if _, err := cache.load(t.Context(), 1, func() ([]ArchiveEntry, error) {
				<-release
				return nil, nil
			}); err != nil {
				t.Error(err)
			}
		}()
		synctest.Wait()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() {
			if _, err := cache.load(ctx, 1, nil); !errors.Is(err, context.Canceled) {
				t.Errorf("waiting reader: %v", err)
			}
		}()
		synctest.Wait()
		cancel()
		synctest.Wait()
		close(release)
		synctest.Wait()
		entries, err := cache.load(t.Context(), 1, nil)
		if err != nil || len(entries) != 0 || !cache.ready {
			t.Fatalf("empty result not cached: %+v, %v", entries, err)
		}
		if _, err := cache.load(ctx, 1, nil); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled cache hit: %v", err)
		}
	})
}

// recentRepository observes bounded reads without supplying the full archive.
type recentRepository struct {
	repository
	read func(context.Context, int) ([]ArchiveEntry, error)
}

func (s recentRepository) NextPublication(context.Context) (time.Time, error) {
	return time.Time{}, nil
}

func (s recentRepository) ListRecent(ctx context.Context, limit int) ([]ArchiveEntry, error) {
	return s.read(ctx, limit)
}

// TestRecentHandlerCache checks recovery, module isolation, and request-specific rendering.
func TestRecentHandlerCache(t *testing.T) {
	h := newHarness(t)
	templates, err := content.PrepareTemplates(h.app.ContentManager, content.TemplatePath{FS: blogModuleFS, Path: pagesFile})
	if err != nil {
		t.Fatal(err)
	}
	view, err := templates.View(h.app.ContentManager, "blog.recent")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	store := recentRepository{read: func(_ context.Context, limit int) ([]ArchiveEntry, error) {
		calls++
		if limit != 3 {
			t.Errorf("query limit=%d, want 3", limit)
		}
		if calls == 1 {
			return nil, errors.New("private storage failure")
		}
		return []ArchiveEntry{{Slug: "one", Title: "One <post>"}}, nil
	}}
	m := &module{app: h.app.ApplicationServer, settings: &Settings{RecentPostCount: 3}, posts: store, views: map[string]*content.View{"recent": view}}
	for i, status := range []int{http.StatusInternalServerError, http.StatusOK, http.StatusOK} {
		r := session.AddSessionManagerToRequestContext(httptest.NewRequest(http.MethodGet, "/posts/recent", nil), h.app.Sessions)
		if i == 2 {
			r.AddCookie(&http.Cookie{Name: returningUserCookie, Value: "1"})
		}
		w := httptest.NewRecorder()
		m.recent(w, r)
		requireStatus(t, w, status)
		body := w.Body.String()
		if strings.Contains(body, "private storage") {
			t.Fatal("storage error exposed")
		}
		if status == http.StatusOK {
			if !strings.Contains(body, "One &lt;post&gt;") {
				t.Fatal("missing escaped post title")
			}
			if strings.Contains(body, `href="/auth/login/email">Log in</a>`) != (i == 2) {
				t.Fatal("cached metadata interfered with request-specific navigation")
			}
		}
	}
	if calls != 2 {
		t.Fatalf("reads=%d, want 2", calls)
	}
	other := &module{app: m.app, settings: m.settings, posts: store, views: m.views}
	r := session.AddSessionManagerToRequestContext(httptest.NewRequest(http.MethodGet, "/posts/recent", nil), h.app.Sessions)
	other.recent(httptest.NewRecorder(), r)
	if calls != 3 {
		t.Fatal("module instances shared cached metadata")
	}
}

// observedPostRepository counts cache misses and can fail writes without changing storage.
// Tests use it only with sequential requests.
type observedPostRepository struct {
	repository
	recentReads int
	writeError  error
}

func (s *observedPostRepository) ListRecent(ctx context.Context, limit int) ([]ArchiveEntry, error) {
	s.recentReads++
	return s.repository.ListRecent(ctx, limit)
}

func (s *observedPostRepository) Save(ctx context.Context, post Post) (Post, error) {
	if s.writeError != nil {
		return Post{}, s.writeError
	}
	return s.repository.Save(ctx, post)
}

func (s *observedPostRepository) Delete(ctx context.Context, id string) error {
	if s.writeError != nil {
		return s.writeError
	}
	return s.repository.Delete(ctx, id)
}

// TestRecentPostChanges checks that committed writes refresh the cache once and
// rejected or failed writes leave it intact, without another storage read.
func TestRecentPostChanges(t *testing.T) {
	h := newHarnessWithRecentPostCount(t, 1)
	store := &observedPostRepository{repository: h.blog.posts}
	h.blog.posts = store
	h.register(t)
	h.verify(t)
	h.login(t)
	read := func(slug, title string, wantReads int) {
		t.Helper()
		before := store.recentReads
		for range 2 {
			w := h.request(http.MethodGet, "/posts/recent", nil, true)
			requireStatus(t, w, http.StatusOK)
			body := w.Body.String()
			if slug == "" {
				if !strings.Contains(body, "No posts yet.") {
					t.Fatal("missing empty recent posts state")
				}
			} else if strings.Count(body, `<li><a href="/post/s/`) != 1 || !strings.Contains(body, `href="/post/s/`+slug+`">`+title+`</a>`) {
				t.Fatalf("unexpected recent posts: %s", body)
			}
		}
		if got := store.recentReads - before; got != wantReads {
			t.Fatalf("storage reads=%d, want %d", got, wantReads)
		}
	}
	read("", "", 1)
	values := url.Values{"slug": {"first"}, "title": {"First"}, "content": {"Body"}}
	requireStatus(t, h.request(http.MethodPost, "/post", values, false), http.StatusSeeOther)
	h.publish(t, values.Get("slug"))
	read("first", "First", 1)
	values.Set("slug", "second")
	values.Set("title", "Second")
	requireStatus(t, h.request(http.MethodPost, "/post", values, false), http.StatusSeeOther)
	h.publish(t, values.Get("slug"))
	read("second", "Second", 1)

	var posts feed
	if err := xml.Unmarshal(h.request(http.MethodGet, "/rss", nil, false).Body.Bytes(), &posts); err != nil || len(posts.Channel.Items) != 2 {
		t.Fatalf("read post IDs: %v", err)
	}
	id := strings.TrimPrefix(posts.Channel.Items[0].GUID, "https://blog.example/post/id/")
	values.Set("slug", "changed")
	values.Set("title", "Edited")
	values.Set("published", "on")
	requireStatus(t, h.submitEditor(t, "/post/"+id, values, false), http.StatusSeeOther)
	read("changed", "Edited", 1)
	requireStatus(t, h.request(http.MethodPost, "/post", values, false), http.StatusConflict)
	read("changed", "Edited", 0)
	values.Set("slug", "first")
	requireStatus(t, h.submitEditor(t, "/post/"+id, values, false), http.StatusConflict)
	read("changed", "Edited", 0)
	values.Set("slug", "changed")
	requireStatus(t, h.request(http.MethodPost, "/post/missing", values, false), http.StatusNotFound)
	read("changed", "Edited", 0)
	requireStatus(t, h.request(http.MethodPost, "/post/delete/missing", nil, false), http.StatusNotFound)
	read("changed", "Edited", 0)
	values.Set("title", "")
	requireStatus(t, h.submitEditor(t, "/post/"+id, values, false), http.StatusUnprocessableEntity)
	read("changed", "Edited", 0)

	store.writeError = errors.New("private write failure")
	values.Set("title", "Unsaved")
	for _, path := range []string{"/post", "/post/" + id, "/post/delete/" + id} {
		requireStatus(t, h.request(http.MethodPost, path, values, false), http.StatusInternalServerError)
		read("changed", "Edited", 0)
	}
	store.writeError = nil

	requireStatus(t, h.request(http.MethodPost, "/post/delete/"+id, nil, false), http.StatusSeeOther)
	read("first", "First", 1)
	id = strings.TrimPrefix(posts.Channel.Items[1].GUID, "https://blog.example/post/id/")
	requireStatus(t, h.request(http.MethodPost, "/post/delete/"+id, nil, false), http.StatusSeeOther)
	read("", "", 1)
}
