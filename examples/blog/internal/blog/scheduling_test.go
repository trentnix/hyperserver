package blog

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPublicationTimeInput(t *testing.T) {
	m := module{now: func() time.Time { return time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC) }}
	for _, test := range []struct {
		input string
		want  string
	}{
		{"9:30 AM", "09:30"},
		{"9:30 PM", "21:30"},
		{"14:30", "14:30"},
		{"00:00", "00:00"},
		{"12:00 AM", "00:00"},
		{"12:00 PM", "12:00"},
		{" 1:07 pm ", "13:07"},
		{"11:59pm", "23:59"},
		{"", ""},
		{"noon", ""},
		{"25:00", ""},
		{"13:00 PM", ""},
		{"9:60 AM", ""},
		{"9:30 AM extra", ""},
	} {
		t.Run(test.input, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/post", nil)
			r.PostForm = url.Values{"published": {"scheduled"}, "publishDate": {"2030-01-02"}, "publishTime": {test.input}}
			post := Post{Draft: true}
			message := m.readPublication(r, &post)
			if test.want == "" {
				if message == "" || !post.Draft || !post.Published.IsZero() {
					t.Fatalf("invalid time changed publication: %q, %+v", message, post)
				}
				return
			}
			if message != "" || post.Draft || post.Published.Format("2006-01-02 15:04 MST") != "2030-01-02 "+test.want+" UTC" {
				t.Fatalf("unexpected schedule: %q, %+v", message, post)
			}
		})
	}
}

// publicationClock advances every publication reader together without sleeping.
func publicationClock(h *harness, start time.Time) func(time.Time) {
	var clock atomic.Int64
	clock.Store(start.UnixNano())
	now := func() time.Time { return time.Unix(0, clock.Load()).UTC() }
	h.blog.now = now
	h.blog.posts.(*sqliteRepository).now = now
	h.blog.feed.now = now
	h.blog.recentPosts.now = now
	h.blog.archivePosts.now = now
	return func(value time.Time) { clock.Store(value.UnixNano()) }
}

// TestScheduledPublication checks every public surface immediately before and at
// publication, including already-warm caches and conditional image/feed requests.
func TestScheduledPublication(t *testing.T) {
	h := newHarness(t)
	start := time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)
	advance := publicationClock(h, start)
	h.register(t)
	h.verify(t)
	h.login(t)
	image, err := h.blog.images.Put(t.Context(), testImageUpload("image/png", testPNG(t)))
	if err != nil {
		t.Fatal(err)
	}
	date := start.Add(time.Hour)
	values := url.Values{"slug": {"scheduled-post"}, "title": {"Scheduled article"}, "content": {"Private scheduled body"}, "previewImage": {image}, "published": {"scheduled"}, "publishDate": {date.Format("2006-01-02")}, "publishTime": {date.Format("15:04")}}
	requireStatus(t, h.request("POST", "/post", values, false), http.StatusSeeOther)
	post, err := h.blog.posts.GetBySlug(t.Context(), "scheduled-post", true)
	if err != nil || !post.Scheduled(start) || post.WasPublished(start) || !post.Published.Equal(date) {
		t.Fatal("schedule not stored", post, err)
	}
	preview := h.request("GET", "/post/s/"+post.Slug, nil, false)
	requireStatus(t, preview, http.StatusOK)
	if !strings.Contains(preview.Body.String(), "Scheduled") || strings.Contains(preview.Body.String(), `property="og:title"`) {
		t.Fatal("scheduled preview missing its state or exposing sharing metadata")
	}
	editor := h.request("GET", "/post/update/"+post.ID, nil, false).Body.String()
	if !strings.Contains(editor, `value="scheduled" selected`) || !strings.Contains(editor, `value="2030-01-01"`) || !strings.Contains(editor, `value="1:00 PM"`) {
		t.Fatal("editor lost its schedule")
	}
	management := h.request("GET", "/post/manage", nil, false).Body.String()
	_, scheduled, _ := strings.Cut(management, `id="scheduled-posts"`)
	scheduled, _, _ = strings.Cut(scheduled, "</section>")
	if !strings.Contains(scheduled, post.Title) {
		t.Fatal("post missing from scheduled section")
	}
	h.cookies = nil
	var oldETag string
	for _, now := range []time.Time{start, date.Add(-time.Nanosecond), date, date.Add(time.Second)} {
		advance(now)
		visible := !now.Before(date)
		status := http.StatusNotFound
		if visible {
			status = http.StatusOK
		}
		for _, path := range []string{"/post/s/" + post.Slug, "/post/id/" + post.ID, "/images/" + image} {
			requireStatus(t, h.request("GET", path, nil, false), status)
		}
		for _, path := range []string{"/", "/rss", "/posts", "/posts/recent", "/sitemap.xml"} {
			response := h.request("GET", path, nil, false)
			requireStatus(t, response, http.StatusOK)
			needle := post.Title
			if path == "/sitemap.xml" {
				needle = "/post/s/" + post.Slug
			}
			if strings.Contains(response.Body.String(), needle) != visible {
				t.Fatalf("%s visibility at %s is incorrect", path, now)
			}
			if path == "/rss" {
				if oldETag == "" {
					oldETag = response.Header().Get("ETag")
				} else if visible && response.Header().Get("ETag") == oldETag {
					t.Fatal("publication did not change the cached feed validator")
				}
			}
		}
	}
	request := httptest.NewRequest("GET", "/rss", nil)
	request.Header.Set("If-None-Match", oldETag)
	response := httptest.NewRecorder()
	h.app.Handler.ServeHTTP(response, request)
	requireStatus(t, response, http.StatusOK)
}

// TestScheduleChanges covers cancellation, rescheduling, early publication, and
// the immutable first-publication date after the scheduled time has arrived.
func TestScheduleChanges(t *testing.T) {
	h := newHarness(t)
	start := time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)
	advance := publicationClock(h, start)
	store := h.blog.posts
	save := func(p Post) Post {
		t.Helper()
		p, err := store.Save(t.Context(), p)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	post := save(Post{Slug: "scheduled", Title: "Title", Markdown: "Body", Published: start.Add(time.Hour)})
	stale := post
	post.Published = start.Add(2 * time.Hour)
	post = save(post)
	stale.Published = start.Add(3 * time.Hour)
	if _, err := store.Save(t.Context(), stale); !errors.Is(err, errEditConflict) {
		t.Fatal("stale editor overwrote the newer schedule", err)
	}
	next, err := store.NextPublication(t.Context())
	if err != nil || !next.Equal(post.Published) {
		t.Fatal("reschedule not reflected", next, err)
	}
	post.Draft = true
	post = save(post)
	if !post.Published.IsZero() {
		t.Fatal("cancelled schedule retained a publication date")
	}
	next, err = store.NextPublication(t.Context())
	if err != nil || !next.IsZero() {
		t.Fatal("cancelled schedule remains pending", next, err)
	}
	post.Draft, post.Published = false, start.Add(time.Hour)
	post = save(post)
	advance(post.Published)
	original := post
	post.Published = start.Add(3 * time.Hour)
	if _, err := store.Save(t.Context(), post); !errors.Is(err, errAlreadyPublished) {
		t.Fatal("rescheduled an already-published post", err)
	}
	stored, err := store.GetByID(t.Context(), post.ID, true)
	if err != nil || stored != original {
		t.Fatal("rejected schedule changed storage", stored, err)
	}
	post = original
	post.Draft = true
	post = save(post)
	advance(start.Add(4 * time.Hour))
	post.Draft = false
	post = save(post)
	if !post.Published.Equal(original.Published) {
		t.Fatal("republishing reset the first publication date")
	}
	early := save(Post{Slug: "early", Title: "Early", Markdown: "Body", Published: start.Add(5 * time.Hour)})
	early, err = store.SetPublished(t.Context(), early.ID, true)
	if err != nil || !early.Published.Equal(start.Add(4*time.Hour)) {
		t.Fatal("publish now did not replace the future schedule", early, err)
	}
}

// TestScheduleValidation keeps invalid schedules out of storage and preserves
// entered values on ordinary errors. HTMX errors leave the mounted editor intact.
func TestScheduleValidation(t *testing.T) {
	h := newHarness(t)
	now := time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)
	publicationClock(h, now)
	h.register(t)
	h.verify(t)
	h.login(t)
	for _, hx := range []bool{false, true} {
		for _, date := range []string{"", "invalid", "2030-01-01T", "T13:00", "2030-01-01T25:00", "2030-02-30T13:00", "2030-01-01T11:59", "2030-01-01T12:00"} {
			day, clock, _ := strings.Cut(date, "T")
			values := url.Values{"title": {"Invalid schedule"}, "content": {"Body"}, "published": {"scheduled"}, "publishDate": {day}, "publishTime": {clock}}
			response := h.request("POST", "/post", values, hx)
			requireStatus(t, response, http.StatusUnprocessableEntity)
			if !strings.Contains(response.Body.String(), "future publication date and time in UTC") || response.Header().Get("HX-Trigger") != "" {
				t.Fatal("invalid schedule did not report a validation error")
			}
			if !hx && !strings.Contains(response.Body.String(), `value="scheduled" selected`) {
				t.Fatal("failed schedule lost its selected publication option")
			}
			if !hx && !strings.Contains(response.Body.String(), `autocomplete="off" value="`+clock+`"`) {
				t.Fatal("failed schedule lost the entered time")
			}
		}
	}
	posts, err := h.blog.posts.ListMetadata(t.Context())
	if err != nil || len(posts) != 0 {
		t.Fatal("invalid schedule wrote content", posts, err)
	}
	values := url.Values{"title": {"Published"}, "content": {"Body"}, "published": {"on"}}
	requireStatus(t, h.request("POST", "/post", values, false), http.StatusSeeOther)
	post, err := h.blog.posts.GetBySlug(t.Context(), "published", true)
	if err != nil {
		t.Fatal(err)
	}
	values.Set("published", "scheduled")
	values.Set("publishDate", "2030-01-01")
	values.Set("publishTime", "13:00")
	for _, hx := range []bool{false, true} {
		response := h.submitEditor(t, "/post/"+post.ID, values, hx)
		requireStatus(t, response, http.StatusUnprocessableEntity)
		if !strings.Contains(response.Body.String(), "already been published") {
			t.Fatal("published post schedule did not explain the restriction")
		}
	}
	stored, err := h.blog.posts.GetByID(t.Context(), post.ID, true)
	if err != nil || stored != post {
		t.Fatal("failed schedule changed published content", stored, err)
	}
}

// TestScheduleEditorWarnings derives warning metadata from saved publication,
// not a rejected edit's requested slug or time. New, unsaved posts never warn.
func TestScheduleEditorWarnings(t *testing.T) {
	h := newHarness(t)
	start := time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)
	advance := publicationClock(h, start)
	h.register(t)
	h.verify(t)
	h.login(t)
	post, err := h.blog.posts.Save(t.Context(), Post{Slug: "scheduled", Title: "Scheduled", Markdown: "Body", Published: start.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	values := url.Values{"slug": {"unsaved-slug"}, "content": {"Body"}, "published": {"scheduled"}, "publishDate": {"2030-01-01"}, "publishTime": {"14:00"}}
	for _, now := range []time.Time{start, post.Published} {
		advance(now)
		editor := h.request("GET", "/post/update/"+post.ID, nil, false)
		requireStatus(t, editor, http.StatusOK)
		failure := h.submitEditor(t, "/post/"+post.ID, values, false)
		requireStatus(t, failure, http.StatusUnprocessableEntity)
		for _, response := range []*httptest.ResponseRecorder{editor, failure} {
			body := response.Body.String()
			if now.Before(post.Published) {
				if !strings.Contains(body, `data-scheduled-slug="scheduled"`) || !strings.Contains(body, `data-scheduled-at="`+strconv.FormatInt(post.Published.Unix(), 10)+`"`) || strings.Contains(body, "data-published-slug=") {
					t.Fatal("scheduled editor lost its saved URL or publication time")
				}
			} else if !strings.Contains(body, `data-published-slug="scheduled"`) || strings.Contains(body, "data-scheduled-slug=") {
				t.Fatal("published editor retained scheduled warning metadata")
			}
		}
	}
	response := h.request("POST", "/post", values, false)
	requireStatus(t, response, http.StatusUnprocessableEntity)
	if strings.Contains(response.Body.String(), "data-scheduled-slug=") || strings.Contains(response.Body.String(), "data-published-slug=") {
		t.Fatal("unsaved post has publication warning metadata")
	}
}

// TestScheduleCacheChanges checks successive deadlines and explicit invalidation
// when an administrator reschedules or cancels the earliest pending publication.
func TestScheduleCacheChanges(t *testing.T) {
	h := newHarness(t)
	start := time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)
	advance := publicationClock(h, start)
	h.register(t)
	h.verify(t)
	h.login(t)
	values := url.Values{"title": {"First"}, "content": {"Body"}, "published": {"scheduled"}, "publishDate": {"2030-01-01"}, "publishTime": {"13:00"}}
	requireStatus(t, h.request("POST", "/post", values, true), http.StatusOK)
	first, err := h.blog.posts.GetBySlug(t.Context(), "first", true)
	if err != nil {
		t.Fatal(err)
	}
	values.Set("title", "Second")
	values.Set("publishTime", "14:00")
	requireStatus(t, h.request("POST", "/post", values, true), http.StatusOK)
	check := func(firstVisible, secondVisible bool) {
		t.Helper()
		for _, path := range []string{"/rss", "/posts/recent", "/posts"} {
			response := h.request("GET", path, nil, false)
			requireStatus(t, response, http.StatusOK)
			body := response.Body.String()
			if strings.Contains(body, "/post/s/first") != firstVisible || strings.Contains(body, "/post/s/second") != secondVisible {
				t.Fatalf("stale %s after schedule change", path)
			}
		}
	}
	check(false, false)
	values.Set("title", "First")
	values.Set("publishTime", "12:30")
	requireStatus(t, h.submitEditor(t, "/post/"+first.ID, values, true), http.StatusOK)
	check(false, false)
	advance(start.Add(30 * time.Minute))
	check(true, false)
	advance(start.Add(2 * time.Hour))
	check(true, true)

	// A newly scheduled post must expire caches that previously had no deadline.
	values.Set("title", "Third")
	values.Set("publishTime", "15:00")
	requireStatus(t, h.request("POST", "/post", values, true), http.StatusOK)
	third, err := h.blog.posts.GetBySlug(t.Context(), "third", true)
	if err != nil {
		t.Fatal(err)
	}
	check(true, true)
	values.Set("published", "")
	requireStatus(t, h.submitEditor(t, "/post/"+third.ID, values, true), http.StatusOK)
	advance(start.Add(3 * time.Hour))
	for _, path := range []string{"/rss", "/posts/recent", "/posts"} {
		response := h.request("GET", path, nil, false)
		requireStatus(t, response, http.StatusOK)
		if strings.Contains(response.Body.String(), "/post/s/third") {
			t.Fatal("cancelled schedule became public")
		}
	}
}

// TestScheduleSurvivesRestart requires no task registration or publication worker.
func TestScheduleSurvivesRestart(t *testing.T) {
	directory := t.TempDir()
	settings := Settings{}
	settings.Storage.Provider, settings.Storage.Connection = "sqlite", "scheduled.db"
	store, closeStore, err := openRepository(t.Context(), settings, directory)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return start }
	_, saveErr := store.Save(t.Context(), Post{Slug: "restart", Title: "Restart", Published: start.Add(time.Hour)})
	if err := closeStore(); err != nil || saveErr != nil {
		t.Fatal(saveErr, err)
	}
	store, closeStore, err = openRepository(t.Context(), settings, directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = closeStore() })
	store.now = func() time.Time { return start.Add(time.Hour) }
	post, err := store.GetBySlug(t.Context(), "restart", false)
	if err != nil || !post.WasPublished(store.now()) {
		t.Fatal("restart lost scheduled publication", post, err)
	}
}

// TestScheduleReadFailure prevents caching incomplete public metadata when
// the next publication time cannot be read. A later healthy request can retry.
func TestScheduleReadFailure(t *testing.T) {
	h := newHarness(t)
	store := h.blog.posts
	h.blog.posts = scheduleReadFailure{repository: store}
	for _, path := range []string{"/rss", "/posts/recent", "/posts"} {
		requireStatus(t, h.request("GET", path, nil, false), http.StatusInternalServerError)
	}
	h.blog.posts = store
	for _, path := range []string{"/rss", "/posts/recent", "/posts"} {
		requireStatus(t, h.request("GET", path, nil, false), http.StatusOK)
	}
}

type scheduleReadFailure struct{ repository }

func (scheduleReadFailure) NextPublication(context.Context) (time.Time, error) {
	return time.Time{}, errors.New("publication lookup failed")
}

// TestCachePublicationBoundary checks expiry coalescing and discards a build that
// crosses its own publication boundary. Failures must not revive expired data.
func TestCachePublicationBoundary(t *testing.T) {
	var clock atomic.Int64
	clock.Store(100)
	cache := cachedValue[string, int]{now: func() time.Time { return time.Unix(clock.Load(), 0) }}
	var builds atomic.Int64
	build := func() (int, time.Time, error) {
		count := builds.Add(1)
		if count == 1 {
			return 1, time.Unix(101, 0), nil
		}
		if count == 2 {
			clock.Store(102)
			return 2, time.Unix(102, 0), nil
		}
		return 3, time.Time{}, nil
	}
	if value, err := cache.loadUntil(t.Context(), "key", build); err != nil || value != 1 {
		t.Fatal(value, err)
	}
	clock.Store(101)
	failed := errors.New("storage unavailable")
	if _, err := cache.loadUntil(t.Context(), "key", func() (int, time.Time, error) { return 0, time.Time{}, failed }); !errors.Is(err, failed) {
		t.Fatal("expired cache hid a read failure", err)
	}
	var workers sync.WaitGroup
	for range 20 {
		workers.Go(func() {
			value, err := cache.loadUntil(t.Context(), "key", build)
			if err != nil || value != 3 {
				t.Errorf("stale publication result: %d, %v", value, err)
			}
		})
	}
	workers.Wait()
	if builds.Load() != 3 {
		t.Fatal("concurrent expiry was not coalesced", builds.Load())
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := cache.loadUntil(ctx, "key", build); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled request used the cache", err)
	}
}
