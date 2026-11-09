package blog

import (
	"context"
	"errors"
	"html"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

// adminWritingContext supplies an authenticated account snapshot for operation tests.
// Content fixtures use persistPost, persistPage, and persistProfile without an actor.
func adminWritingContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, user.UserContextKey, &user.User{ID: "fixture-admin", Role: "admin"})
}

// savePostFixture lets storage tests supply rendered references without applying
// HTTP validation to deliberately incomplete or invalid content fixtures.
func savePostFixture(store repository, ctx context.Context, p Post) (Post, error) {
	refs, err := imageReferences(p.Markdown, "/post/s/"+p.Slug, p.PreviewImage)
	if err != nil {
		return p, err
	}
	return store.Save(ctx, p, refs)
}

// savePageFixture supplies references for storage-level page fixtures.
func savePageFixture(store pageRepository, ctx context.Context, p Page) (Page, error) {
	refs, err := imageReferences(p.Markdown, p.URL(), p.PreviewImage)
	if err != nil {
		return p, err
	}
	return store.SavePage(ctx, p, refs)
}

// setPostPublication changes fixture visibility through the normal storage save.
func setPostPublication(store repository, ctx context.Context, id string, published bool) (Post, error) {
	p, err := store.GetByID(ctx, id, true)
	if err != nil {
		return p, err
	}
	p.Draft, p.Published = !published, time.Time{}
	return savePostFixture(store, ctx, p)
}

// postFields represents an editor submission without dropping existing writing.
func postFields(p Post, published bool) url.Values {
	values := url.Values{"version": {strconv.FormatInt(p.Version, 10)}, "title": {p.Title}, "slug": {p.Slug}, "summary": {p.Summary}, "tags": {p.Tags}, "content": {p.Markdown}, "previewImage": {p.PreviewImage}}
	if published {
		values.Set("published", "on")
	}
	return values
}

// TestPostOversizedDraft keeps bounded invalid writing available for correction.
func TestPostOversizedDraft(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	original, err := h.blog.persistPost(t.Context(), Post{Title: "Original", Slug: "original", Markdown: "Body", Draft: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/post", "/post/" + original.ID} {
		for _, hx := range []bool{false, true} {
			fields := postFields(original, false)
			fields.Set("title", `Unsaved <title> & "notes"`)
			fields.Set("slug", "")
			fields.Set("summary", "Unsaved summary")
			fields.Set("content", "</textarea><script>alert(1)</script>"+strings.Repeat("x", maxMarkdownBytes))
			w := h.request(http.MethodPost, path, fields, hx)
			requireStatus(t, w, http.StatusUnprocessableEntity)
			if w.Header().Get("HX-Trigger") != "" || w.Header().Get("Location") != "" || w.Header().Get("HX-Redirect") != "" {
				t.Fatal("invalid writing acknowledged a save or redirected")
			}
			body := w.Body.String()
			if hx {
				if strings.Contains(body, `id="post-form"`) || !strings.Contains(body, "256 KiB") {
					t.Fatal("HTMX error must retain the mounted editor and explain the limit")
				}
			} else {
				for _, field := range []string{"title", "summary", "content"} {
					if !strings.Contains(body, html.EscapeString(fields.Get(field))) {
						t.Fatalf("ordinary submission lost %s", field)
					}
				}
				if !strings.Contains(body, `name="slug" value=""`) || strings.Contains(body, "<script>alert(1)</script>") {
					t.Fatal("redisplay changed the automatic slug or rendered submitted HTML")
				}
			}
		}
	}
	stored, err := h.blog.posts.GetByID(t.Context(), original.ID, true)
	if err != nil || stored != original {
		t.Fatal("invalid writing changed storage", err)
	}
	if _, err := h.blog.posts.GetBySlug(t.Context(), "unsaved-title-notes", true); !errors.Is(err, errNotFound) {
		t.Fatal("invalid writing created a post", err)
	}
}

// TestPublicationShortcutsRejectUnversionedRequests protects stored writing.
func TestPublicationShortcutsRejectUnversionedRequests(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	p, err := h.blog.persistPost(t.Context(), Post{Title: "Draft", Slug: "draft", Markdown: "Body", Draft: true})
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, h.request(http.MethodPost, "/post/publish/"+p.ID, nil, false), http.StatusBadRequest)
	requireStatus(t, h.request(http.MethodGet, "/post/publish/"+p.ID, nil, false), http.StatusNotFound)
	requireStatus(t, h.request(http.MethodPost, "/post/unpublish/"+p.ID, nil, false), http.StatusNotFound)
	stored, err := h.blog.posts.GetByID(t.Context(), p.ID, true)
	if err != nil || stored != p {
		t.Fatal("invalid publication request changed stored writing", err)
	}
}

// TestRestoreValidatesWriting keeps historical content subject to current writing limits.
func TestRestoreValidatesWriting(t *testing.T) {
	h := newHarness(t)
	for _, kind := range []string{"post", "page"} {
		id, _ := revisionDeleteFixture(t, h, kind)
		before := revisionOwner(t, h, kind, id)
		var version int64
		if kind == "post" {
			version = before.(Post).Version
		} else {
			version = before.(Page).Version
		}
		err := h.blog.restoreRevisionContent(adminWritingContext(t.Context()), kind, id, version, Revision{Title: "Invalid saved writing", Markdown: strings.Repeat("x", maxMarkdownBytes+1)})
		var invalid contentError
		if !errors.As(err, &invalid) {
			t.Fatalf("%s restore bypassed writing validation: %v", kind, err)
		}
		after := revisionOwner(t, h, kind, id)
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("invalid %s restoration changed current content", kind)
		}
	}
}
