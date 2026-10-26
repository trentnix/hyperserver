package blog

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// publish makes a saved fixture public through the same route used by the editor.
func (h *harness) publish(t *testing.T, slug string) Post {
	t.Helper()
	post, err := h.blog.posts.GetBySlug(t.Context(), slug, true)
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, h.request("POST", "/post/publish/"+post.ID, nil, false), http.StatusSeeOther)
	post, err = h.blog.posts.GetByID(t.Context(), post.ID, true)
	if err != nil || post.Draft || post.Published.IsZero() {
		t.Fatal("post was not published", post, err)
	}
	return post
}

// TestPostPublication covers private drafts, both URL forms, caches, and republishing.
func TestPostPublication(t *testing.T) {
	for _, hx := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "htmx"}[hx], func(t *testing.T) {
			h := newHarness(t)
			h.register(t)
			h.verify(t)
			h.login(t)
			status := http.StatusSeeOther
			header := "Location"
			if hx {
				status, header = http.StatusOK, "HX-Redirect"
			}
			values := url.Values{"slug": {"private-post"}, "title": {"Private title"}, "content": {"Private body"}}
			w := h.request("POST", "/post", values, hx)
			requireStatus(t, w, status)
			if w.Header().Get(header) != "/post/s/private-post" {
				t.Fatal("save did not redirect to preview")
			}
			post, err := h.blog.posts.GetBySlug(t.Context(), "private-post", true)
			if err != nil || !post.Draft || !post.Published.IsZero() {
				t.Fatal("save must create an unpublished draft", post, err)
			}
			checkVisibility := func(published bool) {
				t.Helper()
				w := h.request("GET", "/post/manage", nil, hx)
				requireStatus(t, w, http.StatusOK)
				for section, visible := range map[string]bool{"draft-posts": !published, "published-posts": published} {
					_, body, found := strings.Cut(w.Body.String(), `id="`+section+`"`)
					body, _, _ = strings.Cut(body, "</section>")
					if !found || strings.Contains(body, "Private title") != visible {
						t.Fatalf("incorrect %s listing", section)
					}
				}
				if w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("post management is cacheable")
				}
				for _, path := range []string{"/post/s/" + post.Slug, "/post/id/" + post.ID} {
					w := h.request("GET", path, nil, hx)
					requireStatus(t, w, http.StatusOK)
					if w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "Draft preview") == published {
						t.Fatal("incorrect preview state or cache policy")
					}
				}
				cookies := h.cookies
				h.cookies = make(map[string]*http.Cookie)
				defer func() { h.cookies = cookies }()
				for _, path := range []string{"/post/s/" + post.Slug, "/post/id/" + post.ID} {
					want := http.StatusNotFound
					if published {
						want = http.StatusOK
					}
					w := h.request("GET", path, nil, hx)
					requireStatus(t, w, want)
					if !published && (strings.Contains(w.Body.String(), post.Title) || strings.Contains(w.Body.String(), post.Markdown)) {
						t.Fatal("public response leaked a draft")
					}
				}
				for _, path := range []string{"/", "/rss", "/posts", "/posts/recent"} {
					w := h.request("GET", path, nil, false)
					requireStatus(t, w, http.StatusOK)
					if strings.Contains(w.Body.String(), "Private title") != published {
						t.Fatalf("%s has incorrect publication state", path)
					}
				}
			}
			checkVisibility(false) // Warm empty public caches before publication.
			var firstPublished Post
			for _, action := range []string{"publish", "publish", "unpublish", "unpublish", "publish"} {
				w := h.request("POST", "/post/"+action+"/"+post.ID, nil, hx)
				requireStatus(t, w, status)
				if w.Header().Get(header) != "/post/s/private-post" {
					t.Fatal("publication did not redirect")
				}
				stored, err := h.blog.posts.GetByID(t.Context(), post.ID, true)
				if err != nil || stored.Published.IsZero() {
					t.Fatal("missing first publication date", err)
				}
				if firstPublished.ID == "" {
					firstPublished = stored
				} else if !stored.Published.Equal(firstPublished.Published) {
					t.Fatal("publication date changed")
				}
				checkVisibility(action == "publish")
			}
		})
	}
}

// TestSavePostPublication applies the checkbox to new posts and edits in both HTTP modes.
func TestSavePostPublication(t *testing.T) {
	for _, hx := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "htmx"}[hx], func(t *testing.T) {
			h := newHarness(t)
			h.register(t)
			h.verify(t)
			h.login(t)
			status := http.StatusSeeOther
			if hx {
				status = http.StatusOK
			}
			values := url.Values{"slug": {"checkbox-post"}, "title": {"Checkbox post"}, "content": {"Published on its first save"}, "published": {"on"}}
			var post Post
			var firstPublished time.Time
			for _, publish := range []bool{true, false, true} {
				path := "/post"
				if post.ID != "" {
					path += "/" + post.ID
				}
				if publish {
					values.Set("published", "on")
				} else {
					values.Del("published")
				}
				// Warm caches before each change, then verify the resulting public view.
				for _, path := range []string{"/rss", "/posts/recent"} {
					requireStatus(t, h.request("GET", path, nil, false), http.StatusOK)
				}
				response := h.request("POST", path, values, hx)
				requireStatus(t, response, status)
				var err error
				post, err = h.blog.posts.GetBySlug(t.Context(), "checkbox-post", true)
				if err != nil || post.Draft == publish || post.Published.IsZero() {
					t.Fatal("save ignored checkbox or publication date", post, err)
				}
				if firstPublished.IsZero() {
					firstPublished = post.Published
				} else if !post.Published.Equal(firstPublished) {
					t.Fatal("save reset the first publication date")
				}
				editor := h.request("GET", "/post/update/"+post.ID, nil, hx).Body.String()
				if strings.Contains(editor, `name="published" checked`) != publish {
					t.Fatal("editor checkbox does not reflect stored state")
				}
				cookies := h.cookies
				h.cookies = nil
				for _, path := range []string{"/rss", "/posts/recent", "/posts", "/post/s/checkbox-post"} {
					response := h.request("GET", path, nil, false)
					if strings.Contains(response.Body.String(), "Checkbox post") != publish {
						t.Fatalf("%s does not reflect publication state", path)
					}
				}
				h.cookies = cookies
			}

			// Invalid checkbox values must not silently create or unpublish a post.
			values.Set("slug", "invalid-publication")
			values.Set("published", "invalid")
			for _, path := range []string{"/post", "/post/" + post.ID} {
				response := h.request("POST", path, values, hx)
				requireStatus(t, response, http.StatusUnprocessableEntity)
				if !strings.Contains(response.Body.String(), "Choose whether to publish the post.") || response.Header().Get("HX-Trigger") != "" {
					t.Fatal("invalid publication setting must report validation failure without acknowledging a save")
				}
			}
			if _, err := h.blog.posts.GetBySlug(t.Context(), "invalid-publication", true); !errors.Is(err, errNotFound) {
				t.Fatal("invalid publication setting wrote a post", err)
			}
			stored, err := h.blog.posts.GetByID(t.Context(), post.ID, true)
			if err != nil || stored != post {
				t.Fatal("invalid publication setting changed the existing post", stored, err)
			}
			values.Set("slug", post.Slug)

			// Invalid content retains either checkbox choice without changing storage.
			values.Set("content", "")
			for _, publish := range []bool{false, true} {
				if publish {
					values.Set("published", "on")
				} else {
					values.Del("published")
				}
				response := h.request("POST", "/post/"+post.ID, values, hx)
				requireStatus(t, response, http.StatusUnprocessableEntity)
				if !hx && strings.Contains(response.Body.String(), `name="published" checked`) != publish {
					t.Fatal("failed save lost the publication checkbox")
				}
				stored, err := h.blog.posts.GetByID(t.Context(), post.ID, true)
				if err != nil || stored != post {
					t.Fatal("invalid save changed stored content or visibility", stored, err)
				}
			}
		})
	}
}

// TestPostSavePublicationAtomicity keeps content and visibility unchanged on failed writes.
func TestPostSavePublicationAtomicity(t *testing.T) {
	h := newHarness(t)
	store := h.blog.posts
	post, err := store.Save(t.Context(), Post{Slug: "draft", Title: "Draft", Markdown: "Body", Draft: true})
	if err != nil || !post.Published.IsZero() {
		t.Fatal("new draft has a publication date", post, err)
	}
	post.Draft = false
	post, err = store.Save(t.Context(), post)
	if err != nil || post.Draft || post.Published.IsZero() {
		t.Fatal("saving a draft as published did not assign its date", post, err)
	}
	if _, err := store.Save(t.Context(), Post{Slug: "taken", Title: "Other", Markdown: "Body"}); err != nil {
		t.Fatal(err)
	}
	conflict := post
	conflict.Slug, conflict.Title, conflict.Draft = "taken", "Changed", true
	if _, err := store.Save(t.Context(), conflict); !errors.Is(err, errSlugTaken) {
		t.Fatal("expected slug conflict", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.Save(ctx, conflict); !errors.Is(err, context.Canceled) {
		t.Fatal("save ignored cancellation", err)
	}
	stored, err := store.GetByID(t.Context(), post.ID, true)
	if err != nil || stored != post {
		t.Fatal("failed save partially changed content or publication state", stored, err)
	}
}

// TestPostDeletion returns to Posts for drafts and published posts in both HTTP modes.
func TestPostDeletion(t *testing.T) {
	for _, hx := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "htmx"}[hx], func(t *testing.T) {
			h := newHarness(t)
			h.register(t)
			h.verify(t)
			h.login(t)
			status, header := http.StatusSeeOther, "Location"
			if hx {
				status, header = http.StatusOK, "HX-Redirect"
			}
			for _, draft := range []bool{true, false} {
				post, err := h.blog.posts.Save(t.Context(), Post{Slug: "deletable", Title: "Delete me", Markdown: "Body", Draft: draft})
				if err != nil {
					t.Fatal(err)
				}
				path := "/post/delete/" + post.ID
				requireStatus(t, h.request("GET", path, nil, hx), http.StatusOK)
				response := h.request("POST", path, nil, hx)
				requireStatus(t, response, status)
				if response.Header().Get(header) != "/post/manage" || response.Body.Len() != 0 {
					t.Fatal("deletion must navigate to Posts, not render another post", response.Header())
				}
				if _, err := h.blog.posts.GetByID(t.Context(), post.ID, true); !errors.Is(err, errNotFound) {
					t.Fatal("deleted post is still stored", err)
				}
				response = h.request("POST", path, nil, hx)
				requireStatus(t, response, http.StatusNotFound)
				if response.Header().Get("Location") != "" || response.Header().Get("HX-Redirect") != "" {
					t.Fatal("failed deletion must not redirect")
				}
			}
			post, err := h.blog.posts.Save(t.Context(), Post{Slug: "retained", Title: "Keep me", Markdown: "Body", Draft: true})
			if err != nil {
				t.Fatal(err)
			}
			h.blog.posts = &observedPostRepository{repository: h.blog.posts, writeError: errors.New("private deletion failure")}
			response := h.request("POST", "/post/delete/"+post.ID, nil, hx)
			requireStatus(t, response, http.StatusInternalServerError)
			if response.Header().Get("Location") != "" || response.Header().Get("HX-Redirect") != "" || strings.Contains(response.Body.String(), "private deletion failure") {
				t.Fatal("storage failure redirected or exposed private details")
			}
			if _, err := h.blog.posts.GetByID(t.Context(), post.ID, true); err != nil {
				t.Fatal("failed deletion changed the post", err)
			}
		})
	}
}

// TestPostManagement covers empty states, navigation, and both lists together.
func TestPostManagement(t *testing.T) {
	h := newHarness(t)
	if body := h.request("GET", "/", nil, false).Body.String(); strings.Contains(body, `href="/post/manage"`) {
		t.Fatal("anonymous navigation contains post management")
	}
	h.register(t)
	h.verify(t)
	h.login(t)
	w := h.request("GET", "/post/manage", nil, false)
	requireStatus(t, w, http.StatusOK)
	for _, text := range []string{"No drafts.", "No published posts yet.", `href="/post/manage">Posts</a>`, `href="/post/create"`} {
		if !strings.Contains(w.Body.String(), text) {
			t.Fatalf("empty management page missing %q", text)
		}
	}

	for _, slug := range []string{"draft", "published"} {
		post, err := h.blog.posts.Save(t.Context(), Post{Slug: slug, Title: "<" + slug + ">", Markdown: "Private Markdown", Draft: true})
		if err != nil {
			t.Fatal(err)
		}
		if slug == "published" {
			if _, err := h.blog.posts.SetPublished(t.Context(), post.ID, true); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, hx := range []bool{false, true} {
		w := h.request("GET", "/post/manage", nil, hx)
		requireStatus(t, w, http.StatusOK)
		body := w.Body.String()
		for _, text := range []string{`href="/post/s/draft">&lt;draft&gt;</a>`, `href="/post/s/published">&lt;published&gt;</a>`} {
			if !strings.Contains(body, text) {
				t.Fatalf("management page missing %q", text)
			}
		}
		if strings.Contains(body, ">Preview</a>") || strings.Contains(body, ">View</a>") {
			t.Fatal("management actions duplicate the post title links")
		}
		if strings.Contains(body, "Private Markdown") || strings.Contains(body, "No drafts.") || strings.Contains(body, "No published posts yet.") {
			t.Fatal("management page contains post bodies or incorrect empty states")
		}
		if strings.Count(body, `hx-get="/post/update/`) != 2 || strings.Count(body, `hx-get="/post/delete/`) != 2 {
			t.Fatal("each post must have edit and delete actions")
		}
		if strings.Count(body, `action="/post/unpublish/`) != 1 || !strings.Contains(body, `?from=posts"`) || !strings.Contains(body, ">Move to drafts</button>") {
			t.Fatal("only the published post must have a Move to drafts action")
		}
	}
}

// TestMovePostToDrafts returns management actions to the list without allowing open redirects.
func TestMovePostToDrafts(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	values := url.Values{"slug": {"managed-post"}, "title": {"Managed post"}, "content": {"Body"}}
	requireStatus(t, h.request("POST", "/post", values, false), http.StatusSeeOther)
	for _, hx := range []bool{false, true} {
		for _, source := range []string{"posts", "https://attacker.example"} {
			post := h.publish(t, "managed-post")
			path := "/post/unpublish/" + post.ID + "?" + url.Values{"from": {source}}.Encode()
			w := h.request("POST", path, nil, hx)
			status, header := http.StatusSeeOther, "Location"
			if hx {
				status, header = http.StatusOK, "HX-Redirect"
			}
			requireStatus(t, w, status)
			want := "/post/s/" + post.Slug
			if source == "posts" {
				want = "/post/manage"
			}
			if got := w.Header().Get(header); got != want {
				t.Fatalf("redirect = %q, want %q", got, want)
			}
			stored, err := h.blog.posts.GetByID(t.Context(), post.ID, true)
			post.Draft = true
			if err != nil || stored != post {
				t.Fatalf("moving to drafts changed more than visibility: %+v, %v", stored, err)
			}
			w = h.request("GET", "/post/manage", nil, hx)
			requireStatus(t, w, http.StatusOK)
			if strings.Contains(w.Body.String(), `action="/post/unpublish/`) || !strings.Contains(w.Body.String(), "No published posts yet.") {
				t.Fatal("management page still treats the post as published")
			}
		}
	}
}

// TestPostPublicationAuthorization rejects anonymous and unverified publication.
func TestPostPublicationAuthorization(t *testing.T) {
	h := newHarness(t)
	post, err := h.blog.posts.Save(t.Context(), Post{Slug: "draft", Title: "Secret", Markdown: "Body", Draft: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, signedIn := range []bool{false, true} {
		if signedIn {
			h.register(t)
			h.login(t)
		}
		for _, hx := range []bool{false, true} {
			for _, action := range []string{"publish", "unpublish"} {
				w := h.request("POST", "/post/"+action+"/"+post.ID, nil, hx)
				requireStatus(t, w, http.StatusForbidden)
				if w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("authorization failure is cacheable")
				}
			}
			requireStatus(t, h.request("GET", "/post/manage", nil, hx), http.StatusForbidden)
			requireStatus(t, h.request("GET", "/post/id/"+post.ID, nil, hx), http.StatusNotFound)
			requireStatus(t, h.request("GET", "/post/s/draft", nil, hx), http.StatusNotFound)
		}
	}
}

// failedPublicationRepository fails publication and management listing without changing content.
type failedPublicationRepository struct{ repository }

func (failedPublicationRepository) SetPublished(context.Context, string, bool) (Post, error) {
	return Post{}, errors.New("private publication failure")
}

func (failedPublicationRepository) ListMetadata(context.Context) ([]Post, error) {
	return nil, errors.New("private management listing failure")
}

// TestPostPublicationFailures keeps failed operations out of storage and public caches.
func TestPostPublicationFailures(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	values := url.Values{"slug": {"failure"}, "title": {"Failure"}, "content": {"Body"}}
	requireStatus(t, h.request("POST", "/post", values, false), http.StatusSeeOther)
	post := h.publish(t, "failure")
	feed := h.request("GET", "/rss", nil, false)
	requireStatus(t, h.request("GET", "/posts/recent", nil, true), http.StatusOK)
	recent := append([]ArchiveEntry(nil), h.blog.recentPosts.value...)
	store := h.blog.posts
	h.blog.posts = failedPublicationRepository{store}
	for _, hx := range []bool{false, true} {
		for _, action := range []string{"publish", "unpublish"} {
			w := h.request("POST", "/post/"+action+"/"+post.ID, nil, hx)
			requireStatus(t, w, http.StatusInternalServerError)
			if strings.Contains(w.Body.String(), "private publication") || w.Header().Get("Location") != "" || w.Header().Get("HX-Redirect") != "" {
				t.Fatal("publication failure leaked details or redirected")
			}
		}
		w := h.request("GET", "/post/manage", nil, hx)
		requireStatus(t, w, http.StatusInternalServerError)
		if strings.Contains(w.Body.String(), "private management") {
			t.Fatal("post management leaked storage details")
		}
	}
	stored, err := store.GetByID(t.Context(), post.ID, true)
	if err != nil || stored != post {
		t.Fatal("failed publication changed content", err)
	}
	if !h.blog.feed.ready || !h.blog.recentPosts.ready {
		t.Fatal("failed publication invalidated caches")
	}
	if got := h.request("GET", "/rss", nil, false); got.Header().Get("ETag") != feed.Header().Get("ETag") || got.Body.String() != feed.Body.String() {
		t.Fatal("failed publication changed RSS")
	}
	if !reflect.DeepEqual(h.blog.recentPosts.value, recent) {
		t.Fatal("failed publication changed recent posts")
	}
	h.blog.posts = store
	for _, hx := range []bool{false, true} {
		for _, action := range []string{"publish", "unpublish"} {
			requireStatus(t, h.request("POST", "/post/"+action+"/missing", nil, hx), http.StatusNotFound)
		}
	}
}

// TestPostPublicationCSRF rejects state changes through GET and cross-origin POST.
func TestPostPublicationCSRF(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	post, err := h.blog.posts.Save(t.Context(), Post{Slug: "draft", Title: "Secret", Markdown: "Body", Draft: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"publish", "unpublish"} {
		path := "/post/" + action + "/" + post.ID
		if w := h.request("GET", path, nil, false); w.Code == http.StatusOK {
			t.Fatal("GET accepted a publication action")
		}
		r := httptest.NewRequest("POST", "https://blog.example"+path, nil)
		r.Header.Set("Origin", "https://attacker.example")
		for _, cookie := range h.cookies {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.app.Handler.ServeHTTP(w, r)
		requireStatus(t, w, http.StatusForbidden)
	}
	stored, err := h.blog.posts.GetByID(t.Context(), post.ID, true)
	if err != nil || stored != post {
		t.Fatal("rejected request changed the draft", stored, err)
	}
}

// TestPostDraftSchemaUpgrade preserves published content when reopening an older database.
func TestPostDraftSchemaUpgrade(t *testing.T) {
	directory := t.TempDir()
	db, err := sql.Open("sqlite3", filepath.Join(directory, "posts.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE blog_posts (id TEXT PRIMARY KEY, slug TEXT NOT NULL UNIQUE, title TEXT NOT NULL, summary TEXT NOT NULL, markdown TEXT NOT NULL, published INTEGER NOT NULL);
INSERT INTO blog_posts VALUES ('old', 'old', 'Original title', 'Summary', 'Original body', 1700000000)`)
	if closeErr := db.Close(); err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	settings := Settings{}
	settings.Storage.Provider, settings.Storage.Connection = "sqlite", "posts.db"
	for range 2 {
		store, closeStore, err := openRepository(t.Context(), settings, directory)
		if err != nil {
			t.Fatal(err)
		}
		post, err := store.GetByID(t.Context(), "old", false)
		if err != nil || post.Draft || post.Published.Unix() != 1700000000 || post.Title != "Original title" || post.Markdown != "Original body" {
			t.Fatal("upgrade changed published content", post, err)
		}
		if err := closeStore(); err != nil {
			t.Fatal(err)
		}
	}
}

// TestPostPublicationStorage checks concurrent publication, updates, and failure paths.
func TestPostPublicationStorage(t *testing.T) {
	h := newHarness(t)
	store := h.blog.posts
	post, err := store.Save(t.Context(), Post{Slug: "draft", Title: "Draft", Markdown: "Body", Draft: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetByID(t.Context(), post.ID, false); !errors.Is(err, errNotFound) {
		t.Fatal("public lookup exposed draft", err)
	}
	var wg sync.WaitGroup
	results := make(chan Post, 8)
	for range 8 {
		wg.Go(func() {
			p, err := store.SetPublished(t.Context(), post.ID, true)
			if err != nil {
				t.Error(err)
			}
			results <- p
		})
	}
	wg.Wait()
	close(results)
	published := <-results
	for p := range results {
		if p != published {
			t.Fatal("concurrent publication changed the timestamp or content")
		}
	}
	// Save applies the selected state without overwriting the first publication date.
	post.Title = "Edited"
	updated, err := store.Save(t.Context(), post)
	if err != nil || !updated.Draft || !updated.Published.Equal(published.Published) {
		t.Fatal("saving content ignored the selected state or changed the date", updated, err)
	}
	if _, err := store.SetPublished(t.Context(), "missing", true); !errors.Is(err, errNotFound) {
		t.Fatal("publishing created a missing post", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.SetPublished(ctx, post.ID, false); !errors.Is(err, context.Canceled) {
		t.Fatal("publication ignored cancellation", err)
	}
}

// TestPostPublicationDateAndEdits uses an old date so repeated transitions
// cannot hide an accidental timestamp reset by finishing within the same second.
func TestPostPublicationDateAndEdits(t *testing.T) {
	settings := Settings{}
	settings.Storage.Provider, settings.Storage.Connection = "sqlite", "posts.db"
	store, closeStore, err := openRepository(t.Context(), settings, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeStore() })
	post, err := store.Save(t.Context(), Post{Slug: "post", Title: "Post", Markdown: "Body"})
	if err != nil {
		t.Fatal(err)
	}
	firstPublished := time.Unix(1700000000, 0).UTC()
	if _, err := store.db.ExecContext(t.Context(), "UPDATE blog_posts SET draft=0, published=? WHERE id=?", firstPublished.Unix(), post.ID); err != nil {
		t.Fatal(err)
	}
	for _, published := range []bool{true, false, false, true} {
		post, err = store.SetPublished(t.Context(), post.ID, published)
		if err != nil || post.Draft == published || !post.Published.Equal(firstPublished) {
			t.Fatalf("publication %t changed the original date or visibility: %+v, %v", published, post, err)
		}
	}

	// Saving a checked publication setting republishes content without changing its date.
	if _, err := store.SetPublished(t.Context(), post.ID, false); err != nil {
		t.Fatal(err)
	}
	post.Title = "Edited after unpublishing"
	updated, err := store.Save(t.Context(), post)
	if err != nil || updated.Draft || !updated.Published.Equal(firstPublished) || updated.Title != post.Title {
		t.Fatalf("edit ignored publication state or changed the date: %+v, %v", updated, err)
	}
	if _, err := store.GetBySlug(t.Context(), post.Slug, false); err != nil {
		t.Fatalf("published edit is not publicly readable: %v", err)
	}
}
