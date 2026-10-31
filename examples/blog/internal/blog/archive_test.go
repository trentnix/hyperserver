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
	"testing"
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
	store.now = func() time.Time { return now }
	for _, p := range []Post{
		{Slug: "private-draft", Title: "Private draft", Draft: true},
		{Slug: "private-scheduled", Title: "Private scheduled", Published: now.Add(time.Hour)},
	} {
		if _, err := h.blog.posts.Save(t.Context(), p); err != nil {
			t.Fatal(err)
		}
	}
	h.blog.recentPosts.invalidate()
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
				if number == 2 && (!strings.Contains(archive, "September 2026") || !strings.Contains(archive, "October 2026")) {
					t.Fatal("page did not retain month grouping")
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
	if store.limit != archivePageSize+1 || store.offset != archivePageSize || strings.Contains(w.Body.String(), "private archive error") {
		t.Fatalf("incorrect bounded request or exposed error: %+v", store)
	}
}
