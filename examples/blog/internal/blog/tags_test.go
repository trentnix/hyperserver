package blog

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestNormalizeTags covers optional input, canonical labels, duplicate prevention, and bounds.
func TestNormalizeTags(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{" , , ", ""},
		{" Go, go, GO, self-hosting, Home   server ", "go, home server, self-hosting"},
		{"Straße, STRASSE, École, E\u0301COLE", "strasse, école"},
		{strings.Repeat("Go,", 30), "go"},
		{strings.Repeat("é", 50), strings.Repeat("é", 50)},
	} {
		got, err := normalizeTags(tc.input)
		if err != nil || got != tc.want {
			t.Fatalf("normalize %q = %q, %v, want %q", tc.input, got, err, tc.want)
		}
		if again, err := normalizeTags(got); err != nil || again != got {
			t.Fatal("normalization is not idempotent", again, err)
		}
	}
	var many []string
	for i := range 21 {
		many = append(many, fmt.Sprint(i))
	}
	for _, invalid := range []string{strings.Join(many, ","), strings.Repeat("é", 51), strings.Repeat("a", 4097), "a\nb", "a\x00b", string([]byte{0xff})} {
		var message contentError
		if _, err := normalizeTags(invalid); !errors.As(err, &message) {
			t.Fatalf("invalid tags accepted: %q", invalid)
		}
	}
}

// TestTagSearch checks the same ranking for management and readers, without
// exposing draft tags or tags belonging only to saved versions.
func TestTagSearch(t *testing.T) {
	h := newHarness(t)
	store := h.blog.posts.(*sqliteRepository)
	for _, kind := range []string{"post", "page"} {
		for i, field := range []string{"body", "summary", "tags", "title"} {
			content := Revision{Title: kind + " " + field, Markdown: "Body"}
			switch field {
			case "body":
				content.Markdown = "Needle"
			case "summary":
				content.Summary = "Needle"
			case "tags":
				content.Tags = "needle"
			case "title":
				content.Title = "Needle " + content.Title
			}
			slug := fmt.Sprintf("%s-match-%d", kind, i)
			var err error
			if kind == "post" {
				_, err = h.blog.writePost(t.Context(), Post{Slug: slug, Title: content.Title, Tags: content.Tags, Summary: content.Summary, Markdown: content.Markdown})
			} else {
				_, err = h.blog.writePage(t.Context(), Page{Slug: slug, Title: content.Title, Tags: content.Tags, Summary: content.Summary, Markdown: content.Markdown, ChildrenDisplay: "hidden", Published: true})
			}
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	posts, err := store.SearchPosts(t.Context(), "NEEDLE")
	if err != nil {
		t.Fatal(err)
	}
	pages, err := store.SearchPages(t.Context(), "NEEDLE")
	if err != nil {
		t.Fatal(err)
	}
	for i, field := range []string{"title", "tags", "summary", "body"} {
		if len(posts) != 4 || len(pages) != 4 || !strings.HasSuffix(posts[i].Title, field) || !strings.HasSuffix(pages[i].Title, field) {
			t.Fatalf("incorrect management ranking: %v / %v", posts, pages)
		}
	}
	public, err := store.SearchPublic(t.Context(), "NEEDLE")
	if err != nil || public.Total != 8 {
		t.Fatal(public, err)
	}
	for i, field := range []string{"title", "tags", "summary", "body"} {
		if !strings.HasSuffix(public.Items[i*2].Title, field) || !strings.HasSuffix(public.Items[i*2+1].Title, field) {
			t.Fatal("incorrect public ranking", public)
		}
	}
	for _, p := range []Post{
		{Title: "Private", Markdown: "Body", Tags: "secret-tag", Draft: true},
		{Title: "Scheduled", Markdown: "Body", Tags: "secret-tag", Published: time.Now().Add(time.Hour)},
	} {
		if _, err := h.blog.writePost(t.Context(), p); err != nil {
			t.Fatal(err)
		}
	}
	parent, err := h.blog.writePage(t.Context(), Page{Title: "Private page", Tags: "secret-tag", ChildrenDisplay: "hidden"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.blog.writePage(t.Context(), Page{Title: "Hidden child", Tags: "secret-tag", ParentID: parent.ID, Published: true, ChildrenDisplay: "hidden"}); err != nil {
		t.Fatal(err)
	}
	public, err = store.SearchPublic(t.Context(), "secret-tag")
	if err != nil || public.Total != 0 {
		t.Fatal("public search exposed private tags", public, err)
	}
	// Clearing a tag removes its search match and suggestion, not its historical snapshot.
	for _, kind := range []string{"post", "page"} {
		id, version, err := saveRevisionContent(t.Context(), store, kind, "", 0, Revision{Title: "History", Markdown: "Body", Tags: "former-tag"})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := saveRevisionContent(t.Context(), store, kind, id, version, Revision{Title: "History", Markdown: "Body"}); err != nil {
			t.Fatal(err)
		}
	}
	public, err = store.SearchPublic(t.Context(), "former-tag")
	if err != nil || public.Total != 0 {
		t.Fatal("search matched historical tags", public, err)
	}
	choices, err := store.ListTags(t.Context())
	if err != nil || !reflect.DeepEqual(choices, []string{"needle", "secret-tag"}) {
		t.Fatal("suggestions must combine current tags, including drafts, without duplicates", choices, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.ListTags(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("suggestions ignored cancellation", err)
	}
	if err := store.db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListTags(t.Context()); err == nil {
		t.Fatal("suggestions hid storage failure")
	}
}

// TestTagHistory makes tag-only edits recoverable and keeps normalized no-op saves out of history.
func TestTagHistory(t *testing.T) {
	for _, kind := range []string{"post", "page"} {
		t.Run(kind, func(t *testing.T) {
			h := newHarness(t)
			store := h.blog.posts.(*sqliteRepository)
			id, version, err := saveRevisionContent(t.Context(), store, kind, "", 0, Revision{Title: "Tagged", Markdown: "Body", Tags: "go"})
			if err != nil {
				t.Fatal(err)
			}
			first, err := store.ListRevisions(t.Context(), kind, id)
			if err != nil {
				t.Fatal(err)
			}
			older, err := store.GetRevision(t.Context(), kind, id, first[0].ID)
			if err != nil || older.Tags != "go" {
				t.Fatal("tags not recorded", older, err)
			}
			changed := older
			changed.Tags = "sqlite"
			if err := h.blog.restoreRevisionContent(t.Context(), kind, id, version, changed); err != nil {
				t.Fatal(err)
			}
			if err := h.blog.restoreRevisionContent(t.Context(), kind, id, version, older); !errors.Is(err, errEditConflict) {
				t.Fatal("stale tag edit overwrote content", err)
			}
			if err := h.blog.restoreRevisionContent(t.Context(), kind, id, version+1, older); err != nil {
				t.Fatal(err)
			}
			older.Tags = "GO, go"
			if err := h.blog.restoreRevisionContent(t.Context(), kind, id, version+2, older); err != nil {
				t.Fatal(err)
			}
			list, err := store.ListRevisions(t.Context(), kind, id)
			if err != nil || len(list) != 3 {
				t.Fatal("tag history missing or normalized no-op added a revision", list, err)
			}
			restored, err := store.GetRevision(t.Context(), kind, id, list[0].ID)
			if err != nil || restored.Tags != "go" {
				t.Fatal("restore lost tags", restored, err)
			}
		})
	}
}

// TestTagEditors exercises ordinary and HTMX saves, validation, escaped suggestions,
// history loading, and public rendering that does not display tags.
func TestTagEditors(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	for _, kind := range []string{"post", "page"} {
		prefix := "/post"
		if kind == "page" {
			prefix = "/pages"
		}
		for _, hx := range []bool{false, true} {
			slug := fmt.Sprintf("tag-%s-%t", kind, hx)
			fields := url.Values{"title": {"Tagged content"}, "slug": {slug}, "content": {"Body"}, "published": {"on"}, "tags": {"Go, GO, <search-only>"}}
			w := h.request("POST", prefix, fields, hx)
			if w.Code >= 400 {
				t.Fatal(w.Code, w.Body.String())
			}
			var id, tags, path string
			var version int64
			if kind == "post" {
				p, err := h.blog.posts.GetBySlug(t.Context(), slug, true)
				if err != nil {
					t.Fatal(err)
				}
				id, tags, version, path = p.ID, p.Tags, p.Version, "/post/s/"+p.Slug
			} else {
				p, err := h.blog.pageStore.GetPageBySlug(t.Context(), slug, true)
				if err != nil {
					t.Fatal(err)
				}
				id, tags, version, path = p.ID, p.Tags, p.Version, p.URL()
			}
			if tags != "<search-only>, go" {
				t.Fatal("tags were not normalized", tags)
			}
			w = h.request("GET", path, nil, hx)
			requireStatus(t, w, http.StatusOK)
			if strings.Contains(w.Body.String(), "search-only") {
				t.Fatal("public template displays tags")
			}
			list, err := h.blog.history.ListRevisions(t.Context(), kind, id)
			if err != nil {
				t.Fatal(err)
			}
			w = h.request("GET", prefix+"/update/"+id+"?revision="+strconv.FormatInt(list[0].ID, 10), nil, hx)
			requireStatus(t, w, http.StatusOK)
			body := w.Body.String()
			if !strings.Contains(body, `name="tags" value="&lt;search-only&gt;, go"`) || !strings.Contains(body, `<option value="go" data-tag="go">`) || strings.Contains(body, "<search-only>") {
				t.Fatal("editor lost tags or rendered an unsafe suggestion")
			}
			fields.Set("version", strconv.FormatInt(version, 10))
			fields.Set("tags", strings.Repeat("x", 51))
			w = h.request("POST", prefix+"/"+id, fields, hx)
			requireStatus(t, w, http.StatusUnprocessableEntity)
			if !strings.Contains(w.Body.String(), "50 characters") || w.Header().Get("HX-Trigger") != "" {
				t.Fatal("invalid tags reported success")
			}
			if !hx && !strings.Contains(w.Body.String(), `name="tags" value="`+fields.Get("tags")+`"`) {
				t.Fatal("invalid tags lost on ordinary redisplay")
			}
			if hx && strings.Contains(w.Body.String(), `name="tags"`) {
				t.Fatal("HTMX validation replaced the editor")
			}
		}
	}
	h.blog.tags = unavailableTags{}
	for _, path := range []string{"/post/create", "/pages/create"} {
		w := h.request("GET", path, nil, false)
		requireStatus(t, w, http.StatusOK)
		if !strings.Contains(w.Body.String(), `name="tags"`) || strings.Contains(w.Body.String(), "private storage") {
			t.Fatal("optional suggestion failure blocked writing or leaked details")
		}
	}
}

type unavailableTags struct{}

func (unavailableTags) ListTags(context.Context) ([]string, error) {
	return nil, errors.New("private storage failure")
}

// TestTagSchemaUpgrade preserves existing writing and revision IDs across repeated startup.
func TestTagSchemaUpgrade(t *testing.T) {
	h := newHarness(t)
	store := h.blog.posts.(*sqliteRepository)
	for _, kind := range []string{"post", "page"} {
		if _, _, err := saveRevisionContent(t.Context(), store, kind, "", 0, Revision{Title: "Before tags", Markdown: "Body"}); err != nil {
			t.Fatal(err)
		}
		owners, revisions, _ := revisionTable(kind)
		for _, table := range []string{owners, revisions} {
			if _, err := store.db.Exec("ALTER TABLE " + table + " DROP COLUMN tags"); err != nil {
				t.Fatal(err)
			}
		}
	}
	for range 2 {
		if err := initializeRevisions(t.Context(), store.db); err != nil {
			t.Fatal(err)
		}
	}
	for _, kind := range []string{"post", "page"} {
		owners, revisions, _ := revisionTable(kind)
		var count int
		if err := store.db.QueryRow("SELECT count(*) FROM " + revisions + " WHERE title='Before tags' AND tags=''").Scan(&count); err != nil || count != 1 {
			t.Fatal("upgrade changed history", count, err)
		}
		if err := store.db.QueryRow("SELECT count(*) FROM " + owners + " WHERE title='Before tags' AND tags='' AND version=1").Scan(&count); err != nil || count != 1 {
			t.Fatal("upgrade changed writing", count, err)
		}
	}
}
