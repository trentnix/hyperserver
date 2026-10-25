package blog

import (
	"context"
	"errors"
	"html"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func profileFields(name string) url.Values {
	return url.Values{"name": {name}, "bio": {"Notes about <software> & life."}, "imageURL": {"/assets/img/profile-default.svg"}, "faviconURL": {"https://example.test/icon.png"}, "backgroundURL": {"/assets/img/site-bg.jpg"}}
}

// TestProfileImageFields preserves draft URLs and supplies neutral picker previews.
func TestProfileImageFields(t *testing.T) {
	for _, p := range []Profile{
		{},
		{ImageURL: "/images/portrait.png", FaviconURL: "/local-assets/icon.ico", BackgroundURL: "https://example.test/background.jpg"},
	} {
		fields := (profileEditorData{Profile: p}).ImageFields()
		values := map[string]string{"imageURL": p.ImageURL, "faviconURL": p.FaviconURL, "backgroundURL": p.BackgroundURL}
		previews := []string{p.PortraitURL(), p.IconURL(), p.BackgroundURL}
		if len(fields) != 3 {
			t.Fatal("expected three profile image fields")
		}
		for i, field := range fields {
			value, ok := values[field.Name]
			if !ok || field.Value != value || field.PreviewURL() != previews[i] || field.Label == "" {
				t.Fatal("incorrect image field", field)
			}
			delete(values, field.Name)
		}
	}
}

// TestStoredProfile checks authorization, immediate rendering, feed invalidation, and restart persistence.
func TestStoredProfile(t *testing.T) {
	for _, hx := range []bool{false, true} {
		h := newHarness(t)
		for _, method := range []string{"GET", "POST"} {
			requireStatus(t, h.request(method, "/profile", profileFields("Blocked"), hx), http.StatusForbidden)
		}
		h.register(t)
		h.login(t)
		requireStatus(t, h.request("POST", "/profile", profileFields("Unverified"), hx), http.StatusForbidden)
		h.verify(t)
		oldFeed := h.request("GET", "/rss", nil, false)
		fields := profileFields(`A & B <writers>`)
		w := h.request("POST", "/profile", fields, hx)
		status, header := http.StatusSeeOther, "Location"
		if hx {
			status, header = http.StatusOK, "HX-Redirect"
		}
		requireStatus(t, w, status)
		if w.Header().Get(header) != "/profile" {
			t.Fatal("profile save did not reload the document")
		}
		for _, path := range []string{"/about", "/projects", loginURL, "/missing-page", "/profile"} {
			w := h.request("GET", path, nil, false)
			body := w.Body.String()
			if !strings.Contains(body, html.EscapeString(fields.Get("name"))) || !strings.Contains(body, html.EscapeString(fields.Get("bio"))) || !strings.Contains(body, `href="https://example.test/icon.png"`) || !strings.Contains(body, "--site-background:") {
				t.Fatal("stored profile missing or unescaped", path, body)
			}
		}
		feed := h.request("GET", "/rss", nil, false)
		if !strings.Contains(feed.Body.String(), html.EscapeString(fields.Get("name"))) || feed.Header().Get("ETag") == oldFeed.Header().Get("ETag") {
			t.Fatal("profile change did not refresh feed")
		}
		cfg := *h.app.Config
		if err := h.app.Shutdown(); err != nil {
			t.Fatal(err)
		}
		h = startHarness(t, cfg)
		if h.blog.profile.Current().Name != fields.Get("name") {
			t.Fatal("profile did not survive restart")
		}
	}
}

// TestProfileValidation rejects invalid text and URLs without changing published content.
func TestProfileValidation(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	original := h.blog.profile.Current()
	for _, tt := range []struct{ field, value string }{
		{"name", " "}, {"name", strings.Repeat("x", 241)}, {"name", string([]byte{0xff})},
		{"bio", strings.Repeat("x", 501)}, {"bio", string([]byte{0xff})},
		{"imageURL", "javascript:alert(1)"}, {"faviconURL", "//other.test/icon"},
		{"backgroundURL", "http://other.test/background"}, {"backgroundURL", "https://user:pass@other.test/image"},
		{"imageURL", strings.Repeat("x", 2049)},
	} {
		for _, hx := range []bool{false, true} {
			fields := profileFields("Unsaved name")
			fields.Set(tt.field, tt.value)
			w := h.request("POST", "/profile", fields, hx)
			requireStatus(t, w, http.StatusUnprocessableEntity)
			if hx == strings.Contains(w.Body.String(), `id="profile-form"`) {
				t.Fatal("wrong error rendering mode")
			}
			if !hx && tt.field != "name" && !strings.Contains(w.Body.String(), "Unsaved name") {
				t.Fatal("validation lost draft")
			}
		}
	}
	if h.blog.profile.Current() != original {
		t.Fatal("validation changed live profile")
	}
	stored, err := h.blog.profileStore.GetProfile(t.Context())
	if err != nil || stored != original {
		t.Fatal("validation changed stored profile", err)
	}
	for _, image := range []string{"", "/local-assets/icon.ico", "https://example.test/image.png"} {
		p := Profile{Name: "Example Author", ImageURL: image, FaviconURL: image, BackgroundURL: image}
		if err := p.validate(); err != nil {
			t.Fatal(err)
		}
	}
	requireStatus(t, h.request("POST", "/profile", url.Values{"name": {"Minimal"}}, false), http.StatusSeeOther)
	p := h.blog.profile.Current()
	if p.Bio != "" || p.BackgroundURL != "" || p.PortraitURL() != "/assets/img/profile-default.svg" || p.IconURL() != "/assets/img/favicon.svg" {
		t.Fatal("clearing optional fields did not restore defaults")
	}
}

type failingProfileStore struct{ profileRepository }

func (failingProfileStore) SaveProfile(context.Context, Profile) error {
	return errors.New("private profile failure")
}

// TestProfileSaveFailure leaves storage, rendering, and the feed at their last successful values.
func TestProfileSaveFailure(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	original := h.blog.profile.Current()
	feed := h.request("GET", "/rss", nil, false).Body.String()
	h.blog.profileStore = failingProfileStore{h.blog.profileStore}
	for _, hx := range []bool{false, true} {
		w := h.request("POST", "/profile", profileFields("Unsaved name"), hx)
		requireStatus(t, w, http.StatusInternalServerError)
		if strings.Contains(w.Body.String(), "private profile failure") || w.Header().Get("HX-Redirect") != "" || w.Header().Get("Location") != "" {
			t.Fatal("incorrect failed-save response")
		}
		if !hx && !strings.Contains(w.Body.String(), "Unsaved name") {
			t.Fatal("save failure lost draft")
		}
	}
	if h.blog.profile.Current() != original || h.request("GET", "/rss", nil, false).Body.String() != feed {
		t.Fatal("failed save changed public content")
	}
}

// TestProfileConcurrentSaves checks that persisted and published values stay in the same order.
func TestProfileConcurrentSaves(t *testing.T) {
	h := newHarness(t)
	var wg sync.WaitGroup
	for _, name := range []string{"First", "Second", "Third", "Fourth"} {
		wg.Go(func() {
			if err := h.blog.writeProfile(t.Context(), Profile{Name: name, Bio: name}); err != nil {
				t.Error(err)
			}
			p := h.blog.profile.Current()
			if p.Name != p.Bio {
				t.Error("torn profile snapshot")
			}
		})
	}
	wg.Wait()
	stored, err := h.blog.profileStore.GetProfile(t.Context())
	if err != nil || stored != h.blog.profile.Current() {
		t.Fatal("database and rendering disagree", err)
	}
}

// TestProfileStorage checks missing records, replacement, cancellation, and read failures.
func TestProfileStorage(t *testing.T) {
	settings := Settings{}
	settings.Storage.Provider, settings.Storage.Connection = "sqlite", "profile.db"
	store, closeStore, err := openRepository(t.Context(), settings, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore()
	p, err := store.GetProfile(t.Context())
	if !errors.Is(err, errNotFound) || p != (Profile{}) {
		t.Fatal("missing profile was not reported", p, err)
	}
	for _, name := range []string{"First", "Second"} {
		if err := store.SaveProfile(t.Context(), Profile{Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := store.SaveProfile(ctx, Profile{Name: "Cancelled"}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := store.GetProfile(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if p, err := store.GetProfile(t.Context()); err != nil || p.Name != "Second" {
		t.Fatal("failed save changed profile", err)
	}
	closeStore()
	if _, err := store.GetProfile(t.Context()); err == nil || errors.Is(err, errNotFound) {
		t.Fatal("read failure was hidden", err)
	}
}

// TestProfileRefreshDuringFeedBuild prevents an in-flight build from caching stale identity.
func TestProfileRefreshDuringFeedBuild(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	started, release := make(chan struct{}), make(chan struct{}, 1)
	defer close(release)
	result := make(chan string, 1)
	go func() {
		first := true
		doc, err := h.blog.feed.load(ctx, "test", func() ([]byte, error) {
			p := h.blog.profile.Current()
			if first {
				first = false
				close(started)
				select {
				case <-release:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			return []byte(p.Name), nil
		})
		if err != nil {
			result <- err.Error()
			return
		}
		result <- string(doc.data)
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := h.blog.writeProfile(ctx, Profile{Name: "New identity"}); err != nil {
		t.Fatal(err)
	}
	release <- struct{}{}
	select {
	case value := <-result:
		if value != "New identity" {
			t.Fatal("stale feed survived profile edit", value)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

// TestProfileInitialization rejects corrupt stored data without publishing a fallback profile.
func TestProfileInitialization(t *testing.T) {
	h := newHarness(t)
	if err := h.blog.profileStore.SaveProfile(t.Context(), Profile{}); err != nil {
		t.Fatal(err)
	}
	m := &module{settings: h.blog.settings, access: &Access{}, profile: &ProfileState{}}
	if err := m.Init(t.Context(), h.blog.app); err == nil || !strings.Contains(err.Error(), "load blog profile") {
		t.Fatal("invalid profile did not fail startup", err)
	}
	if m.close != nil || m.profile.Current() != (Profile{}) {
		t.Fatal("failed initialization published state")
	}
}

// TestProfileApplicationIsolation keeps one site's edits out of another site's rendering.
func TestProfileApplicationIsolation(t *testing.T) {
	first, second := newHarness(t), newHarness(t)
	before := second.blog.profile.Current()
	if err := first.blog.writeProfile(t.Context(), Profile{Name: "Independent site"}); err != nil {
		t.Fatal(err)
	}
	if second.blog.profile.Current() != before {
		t.Fatal("profile leaked between applications")
	}
}

// TestFreshProfile renders neutral public content when no profile has been saved.
func TestFreshProfile(t *testing.T) {
	h := newHarness(t)
	store := h.blog.profileStore.(*sqliteRepository)
	if _, err := store.db.ExecContext(t.Context(), "DELETE FROM blog_profile"); err != nil {
		t.Fatal(err)
	}
	cfg := *h.app.Config
	if err := h.app.Shutdown(); err != nil {
		t.Fatal(err)
	}
	h = startHarness(t, cfg)
	if _, err := h.blog.profileStore.GetProfile(t.Context()); !errors.Is(err, errNotFound) {
		t.Fatal("initialization unexpectedly persisted a default profile", err)
	}
	w := h.request("GET", "/", nil, false)
	requireStatus(t, w, http.StatusOK)
	body := w.Body.String()
	for _, want := range []string{"<title>My Blog</title>", "/assets/img/profile-default.svg", "/assets/img/favicon.svg"} {
		if !strings.Contains(body, want) {
			t.Fatal("missing fresh-install default", want)
		}
	}
	for _, unwanted := range []string{"Trent Nix", "technologist", "--site-background:"} {
		if strings.Contains(body, unwanted) {
			t.Fatal("fresh profile retained personal content", unwanted)
		}
	}
}

// blockedProfileStore pauses storage so readers can be checked before a save finishes.
type blockedProfileStore struct {
	profileRepository
	started, release chan struct{}
	failure          error
}

func (s blockedProfileStore) SaveProfile(ctx context.Context, p Profile) error {
	close(s.started)
	select {
	case <-s.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	if s.failure != nil {
		return s.failure
	}
	return s.profileRepository.SaveProfile(ctx, p)
}

// TestProfileReadsDuringSave keeps the last published profile available during I/O.
func TestProfileReadsDuringSave(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "success"
		if fail {
			name = "failure"
		}
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			original := h.blog.profile.Current()
			oldFeed := h.request("GET", "/rss", nil, false).Body.String()
			store := blockedProfileStore{profileRepository: h.blog.profileStore, started: make(chan struct{}), release: make(chan struct{})}
			if fail {
				store.failure = errors.New("profile write failed")
			}
			h.blog.profileStore = store

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			done := make(chan struct{})
			var saveError error
			updated := Profile{Name: "Updated site", Bio: "Updated bio"}
			go func() {
				saveError = h.blog.writeProfile(ctx, updated)
				close(done)
			}()
			t.Cleanup(func() { cancel(); <-done })
			select {
			case <-store.started:
			case <-ctx.Done():
				t.Fatal("save did not reach storage")
			}

			read := make(chan Profile, 1)
			go func() { read <- h.blog.profile.Current() }()
			select {
			case p := <-read:
				if p != original {
					t.Fatal("uncommitted profile was published")
				}
			case <-ctx.Done():
				t.Fatal("profile read blocked behind storage")
			}
			if feed := h.request("GET", "/rss", nil, false).Body.String(); feed != oldFeed {
				t.Fatal("in-flight save changed the published feed")
			}

			close(store.release)
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("save did not finish")
			}
			if !errors.Is(saveError, store.failure) {
				t.Fatal("unexpected save error", saveError)
			}
			want := updated
			if fail {
				want = original
			}
			stored, err := h.blog.profileStore.GetProfile(t.Context())
			if err != nil || stored != want || h.blog.profile.Current() != want {
				t.Fatal("stored and published profile disagree", err)
			}
			newFeed := h.request("GET", "/rss", nil, false).Body.String()
			if (newFeed == oldFeed) != fail {
				t.Fatal("incorrect feed invalidation after save")
			}
		})
	}
}
