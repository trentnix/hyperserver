package blog

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// seedArchive creates a stable public order and private posts that must not affect pagination.
func seedArchive(t *testing.T, h *harness, count int) {
	t.Helper()
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	store := h.blog.posts.(*sqliteRepository)
	for i := range count {
		published := now.Add(-time.Duration(i) * 24 * time.Hour)
		store.now = func() time.Time { return published }
		_, err := h.blog.posts.Save(t.Context(), Post{
			Slug: fmt.Sprintf("archive-%03d", i), Title: fmt.Sprintf("Archive <%03d>", i),
			Markdown: "Body not needed by the archive",
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	publicationClock(h, now)
	for _, p := range []Post{
		{Slug: "private-draft", Title: "Private draft", Draft: true},
		{Slug: "private-scheduled", Title: "Private scheduled", Published: now.Add(time.Hour)},
	} {
		if _, err := h.blog.posts.Save(t.Context(), p); err != nil {
			t.Fatal(err)
		}
	}
	h.blog.recentPosts.invalidate()
	h.blog.archivePosts.invalidate()
}

// TestArchivePagination covers ordinary navigation, HTMX, visibility, month groups,
// canonical URLs, boundary pages, and invalid requests.
func TestArchivePagination(t *testing.T) {
	h := newHarness(t)
	seedArchive(t, h, 2*archivePageSize+1)
	links := regexp.MustCompile(`<li><a href="/post/s/([^"]+)">`)
	for _, admin := range []bool{false, true} {
		if admin {
			h.register(t)
			h.verify(t)
			h.login(t)
		}
		for _, hx := range []bool{false, true} {
			for number := 1; number <= 3; number++ {
				w := h.request(http.MethodGet, "/posts?page="+strconv.Itoa(number)+"&tracking=discard", nil, hx)
				requireStatus(t, w, http.StatusOK)
				body := w.Body.String()
				_, archive, found := strings.Cut(body, `<section class="post-archive info-page"`)
				if !found {
					t.Fatal("missing archive")
				}
				archive, _, _ = strings.Cut(archive, "</section>")
				matches := links.FindAllStringSubmatch(archive, -1)
				want := archivePageSize
				if number == 3 {
					want = 1
				}
				if len(matches) != want {
					t.Fatalf("page %d contains %d links, want %d", number, len(matches), want)
				}
				for i, match := range matches {
					if match[1] != fmt.Sprintf("archive-%03d", (number-1)*archivePageSize+i) {
						t.Fatalf("page %d entry %d = %s", number, i, match[1])
					}
				}
				if strings.Contains(archive, "Private") || strings.Contains(archive, "Body not needed") || !strings.Contains(archive, "Archive &lt;") {
					t.Fatal("archive leaked private content or bodies, or failed to escape titles")
				}
				if strings.Contains(archive, `rel="prev"`) != (number > 1) || strings.Contains(archive, `rel="next"`) != (number < 3) {
					t.Fatalf("incorrect adjacent links on page %d", number)
				}
				if number == 2 {
					for _, days := range []int{archivePageSize, 2*archivePageSize - 1} {
						month := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC).AddDate(0, 0, -days).Format("January 2006")
						if !strings.Contains(archive, month) {
							t.Fatalf("page did not retain month grouping: %s", month)
						}
					}
				}
				if !hx {
					canonical := "https://blog.example" + archiveURL(number)
					if !strings.Contains(body, `<link rel="canonical" href="`+canonical+`">`) || !strings.Contains(body, `<meta property="og:url" content="`+canonical+`">`) {
						t.Fatalf("missing normalized page-specific canonical URL: %s", canonical)
					}
				}
			}
		}
	}
	for _, raw := range []string{"0", "-1", "abc", "1.5", "999999999999999999999999999", strconv.Itoa(math.MaxInt)} {
		requireStatus(t, h.request(http.MethodGet, "/posts?page="+raw, nil, true), http.StatusBadRequest)
	}
	for _, hx := range []bool{false, true} {
		requireStatus(t, h.request(http.MethodGet, "/posts?page=4", nil, hx), http.StatusNotFound)
	}
	r := httptest.NewRequest(http.MethodGet, "/posts?page=2", nil)
	r.Header.Set("HX-Request", "true")
	r.Header.Set("HX-History-Restore-Request", "true")
	w := httptest.NewRecorder()
	h.app.Handler.ServeHTTP(w, r)
	requireStatus(t, w, http.StatusOK)
	if !strings.Contains(w.Body.String(), `<link rel="canonical" href="https://blog.example/posts?page=2">`) {
		t.Fatal("history restore lost its page-specific canonical URL")
	}
}

// TestArchiveBoundaries checks empty and exactly-full archives without phantom pages.
func TestArchiveBoundaries(t *testing.T) {
	for _, count := range []int{0, 1, archivePageSize, archivePageSize + 1} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			h := newHarness(t)
			seedArchive(t, h, count)
			w := h.request(http.MethodGet, "/posts", nil, true)
			requireStatus(t, w, http.StatusOK)
			body := w.Body.String()
			if strings.Contains(body, `rel="next"`) != (count > archivePageSize) || strings.Contains(body, "No posts yet.") != (count == 0) {
				t.Fatal("incorrect empty or next-page state")
			}
			wantControls := 0
			if count > archivePageSize {
				wantControls = 1
			}
			if strings.Count(body, `aria-label="Archive pages"`) != wantControls {
				t.Fatal("show one navigation row only when the archive has multiple pages")
			}
			if count <= archivePageSize {
				requireStatus(t, h.request(http.MethodGet, "/posts?page=2", nil, true), http.StatusNotFound)
			}
		})
	}
}

type observedArchive struct {
	repository
	limit, offset int
	err           error
}

func (s *observedArchive) ListArchivePage(ctx context.Context, limit, offset int) ([]ArchiveEntry, error) {
	s.limit, s.offset = limit, offset
	return nil, s.err
}

// TestArchiveBoundedRead ensures pagination reaches storage, and read failures stay failures.
func TestArchiveBoundedRead(t *testing.T) {
	h := newHarness(t)
	store := &observedArchive{repository: h.blog.posts, err: errors.New("private archive error")}
	h.blog.posts = store
	w := h.request(http.MethodGet, "/posts?page=2", nil, true)
	requireStatus(t, w, http.StatusInternalServerError)
	if store.limit != archivePageSize*archiveCachePages+1 || store.offset != 0 || strings.Contains(w.Body.String(), "private archive error") {
		t.Fatalf("incorrect bounded request or exposed error: %+v", store)
	}
}

// archiveCacheRepository isolates cache reads from storage and HTTP rendering.
type archiveCacheRepository struct {
	repository
	read func(context.Context, int, int) ([]ArchiveEntry, error)
	next func(context.Context) (time.Time, error)
}

func (s archiveCacheRepository) ListArchivePage(ctx context.Context, limit, offset int) ([]ArchiveEntry, error) {
	return s.read(ctx, limit, offset)
}

func (s archiveCacheRepository) NextPublication(ctx context.Context) (time.Time, error) {
	if s.next != nil {
		return s.next(ctx)
	}
	return time.Time{}, nil
}

// TestArchiveCacheBlocks checks page slices, lookahead, bounded reads, and eviction.
func TestArchiveCacheBlocks(t *testing.T) {
	const blockSize = archivePageSize * archiveCachePages
	entries := make([]ArchiveEntry, 2*blockSize+1)
	for i := range entries {
		entries[i].Slug = strconv.Itoa(i)
	}
	reads, deadlines := 0, 0
	store := archiveCacheRepository{next: func(context.Context) (time.Time, error) {
		deadlines++
		return time.Time{}, nil
	}, read: func(_ context.Context, limit, offset int) ([]ArchiveEntry, error) {
		reads++
		if limit != blockSize+1 || offset%blockSize != 0 {
			t.Fatalf("unbounded or misaligned read: limit=%d, offset=%d", limit, offset)
		}
		start := min(offset, len(entries))
		return entries[start:min(start+limit, len(entries))], nil
	}}
	m := module{posts: store}
	for _, tc := range []struct{ page, reads int }{
		{1, 1}, {2, 1}, {10, 1}, {11, 2}, {12, 2}, {20, 2}, {21, 3}, {22, 3}, {1, 4},
	} {
		posts, err := m.loadArchivePage(t.Context(), tc.page)
		start := min((tc.page-1)*archivePageSize, len(entries))
		want := min(archivePageSize+1, len(entries)-start)
		if err != nil || len(posts) != want || reads != tc.reads || deadlines != reads || len(m.archivePosts.value) > blockSize+1 {
			t.Fatalf("page %d: entries=%d, reads=%d, err=%v", tc.page, len(posts), reads, err)
		}
		for i, p := range posts {
			if p.Slug != strconv.Itoa(start+i) {
				t.Fatalf("wrong entry on page %d: %s", tc.page, p.Slug)
			}
		}
	}
	other := module{posts: store}
	if _, err := other.loadArchivePage(t.Context(), 1); err != nil || reads != 5 {
		t.Fatal("application instances shared archive state", err)
	}
}

// TestArchiveCacheFailures checks retry, empty snapshots, and cancelled cache hits.
func TestArchiveCacheFailures(t *testing.T) {
	failure := errors.New("storage failed")
	for _, phase := range []string{"deadline", "metadata"} {
		t.Run(phase, func(t *testing.T) {
			fail, reads := true, 0
			m := module{posts: archiveCacheRepository{
				next: func(context.Context) (time.Time, error) {
					if fail && phase == "deadline" {
						return time.Time{}, failure
					}
					return time.Time{}, nil
				},
				read: func(context.Context, int, int) ([]ArchiveEntry, error) {
					reads++
					if fail {
						return nil, failure
					}
					return nil, nil
				},
			}}
			for range 2 {
				if _, err := m.loadArchivePage(t.Context(), 1); !errors.Is(err, failure) || m.archivePosts.ready {
					t.Fatal("failed read became cached content", err)
				}
			}
			if phase == "deadline" && reads != 0 {
				t.Fatal("read content without a valid publication deadline")
			}
			fail, reads = false, 0
			for _, page := range []int{1, 2, 1} {
				if posts, err := m.loadArchivePage(t.Context(), page); err != nil || len(posts) != 0 || reads != 1 {
					t.Fatal("empty archive was not cached", posts, err, reads)
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if _, err := m.loadArchivePage(ctx, 1); !errors.Is(err, context.Canceled) {
				t.Fatal("cache hit ignored cancellation", err)
			}
		})
	}
}

// TestArchiveCacheConcurrentRead checks shared builds across pages and invalidation
// while a read is in flight. Readers must never receive the invalidated snapshot.
func TestArchiveCacheConcurrentRead(t *testing.T) {
	for _, invalidate := range []bool{false, true} {
		t.Run(strconv.FormatBool(invalidate), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var calls atomic.Int32
				release := make(chan struct{})
				m := module{posts: archiveCacheRepository{read: func(context.Context, int, int) ([]ArchiveEntry, error) {
					title := "New"
					if calls.Add(1) == 1 {
						<-release
						title = "Old"
					}
					entries := make([]ArchiveEntry, 2*archivePageSize)
					for i := range entries {
						entries[i].Title = title
					}
					return entries, nil
				}}}
				results := make(chan string, 20)
				for i := range cap(results) {
					go func() {
						posts, err := m.loadArchivePage(t.Context(), i%2+1)
						if err != nil || len(posts) < archivePageSize {
							t.Error("failed concurrent read", err)
							results <- "failure"
							return
						}
						results <- posts[0].Title
					}()
				}
				synctest.Wait()
				if calls.Load() != 1 {
					t.Fatal("concurrent pages started multiple reads")
				}
				want, wantCalls := "Old", int32(1)
				if invalidate {
					m.archivePosts.invalidate()
					want, wantCalls = "New", 2
				}
				close(release)
				synctest.Wait()
				for range cap(results) {
					if got := <-results; got != want {
						t.Fatalf("received %s, want %s", got, want)
					}
				}
				if calls.Load() != wantCalls {
					t.Fatal("unexpected rebuild count", calls.Load())
				}
			})
		})
	}
}
