package blog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestVersionRelativeLinksFollowURL(t *testing.T) {
	for _, kind := range []string{"post", "page"} {
		t.Run(kind, func(t *testing.T) {
			db, _ := schemaTestDB(t)
			if err := prepareBlogSchema(t.Context(), db); err != nil {
				t.Fatal(err)
			}
			s := &sqliteRepository{db: db, now: time.Now}
			body := "[Other](other) [Section](#section)"
			var id, path string
			if kind == "post" {
				p, err := savePostFixture(s, t.Context(), Post{Slug: "old", Title: "Title", Markdown: body, Draft: true})
				if err != nil {
					t.Fatal(err)
				}
				p.Slug = "new"
				p, err = savePostFixture(s, t.Context(), p)
				if err != nil {
					t.Fatal(err)
				}
				id, path = p.ID, "/post/s/new"
			} else {
				p, err := savePageFixture(s, t.Context(), Page{Slug: "ordinary", Title: "Title", Markdown: body})
				if err != nil {
					t.Fatal(err)
				}
				p.Slug = "about"
				p, err = savePageFixture(s, t.Context(), p)
				if err != nil {
					t.Fatal(err)
				}
				id, path = p.ID, p.URL()
			}
			versions, err := s.ListRevisions(t.Context(), kind, id)
			if err != nil || len(versions) != 2 {
				t.Fatal(versions, err)
			}
			current, err := s.GetRevision(t.Context(), kind, id, versions[0].ID)
			if err != nil || current.ContentPath != path {
				t.Fatal(current, err)
			}
			previous, err := s.GetRevision(t.Context(), kind, id, versions[1].ID)
			if err != nil || previous.ContentPath == path {
				t.Fatal(previous, err)
			}
			rendered, err := markdownAt(current.Markdown, &url.URL{Path: current.ContentPath})
			if err != nil || !strings.Contains(string(rendered), `href="`+path+`#section"`) {
				t.Fatal(rendered, err)
			}
		})
	}
}

func TestPublicImageQueryUsesIndexes(t *testing.T) {
	db, _ := schemaTestDB(t)
	if err := prepareBlogSchema(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	rows, err := db.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+fmt.Sprintf(publicMediaQuery, "?"), "/images/missing.png", time.Now().Unix(), "/images/missing.png")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(detail + "\n")
		// The visible-page CTE traverses the hierarchy separately. Neither
		// reference lookup should scan current content or all versions.
		if detail == "SCAN p" || detail == "SCAN r" || detail == "SCAN i" {
			t.Errorf("unindexed image lookup: %s", detail)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, index := range []string{"blog_post_media_url", "blog_page_media_url"} {
		if !strings.Contains(plan.String(), index) {
			t.Errorf("missing %s lookup:\n%s", index, plan.String())
		}
	}
}

func TestVersionReferencesFollowPageURL(t *testing.T) {
	h := newHarness(t)
	s := h.blog.posts.(*sqliteRepository)
	page, err := savePageFixture(s, t.Context(), Page{Slug: "ordinary", Title: "Relative", Markdown: "![Image](images/x.png)", Published: true})
	if err != nil {
		t.Fatal(err)
	}
	// /page/ordinary resolves images/x.png outside the upload route.
	// The harness already owns /projects. Use /about after deleting its fixture.
	about, err := s.GetPageBySlug(t.Context(), "about", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeletePage(t.Context(), about.ID); err != nil {
		t.Fatal(err)
	}
	page.Slug = "about"
	page, err = savePageFixture(s, t.Context(), page)
	if err != nil {
		t.Fatal(err)
	}
	if public, err := s.MediaIsPublic(t.Context(), "/images/x.png"); err != nil || !public {
		t.Fatal(public, err)
	}
	versions, err := s.ListRevisions(t.Context(), "page", page.ID)
	if err != nil || len(versions) != 2 {
		t.Fatal(versions, err)
	}
	old, err := s.GetRevision(t.Context(), "page", page.ID, versions[1].ID)
	if err != nil || old.ContentPath != "/page/ordinary" {
		t.Fatal("historical relative links lost their URL base", old, err)
	}
	var n int
	if err := s.db.QueryRow("SELECT count(*) FROM blog_page_media WHERE version_id=?", versions[1].ID).Scan(&n); err != nil || n != 0 {
		t.Fatal("old references changed", n, err)
	}
}

func TestVersionPointerConstraints(t *testing.T) {
	settings := Settings{}
	settings.Storage.Provider, settings.Storage.Connection = "sqlite", "versions.db"
	s, closeStore, err := openRepository(t.Context(), settings, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore()
	a, err := savePostFixture(s, t.Context(), Post{Slug: "a", Title: "A", Markdown: "![old](/images/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.png)"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := savePostFixture(s, t.Context(), Post{Slug: "b", Title: "B", Markdown: "B"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("UPDATE blog_current_posts SET version_id=(SELECT version_id FROM blog_current_posts WHERE id=?) WHERE id=?", b.ID, a.ID); err == nil {
		t.Fatal("accepted another post's version")
	}
	old, err := s.ListRevisions(t.Context(), "post", a.ID)
	if err != nil {
		t.Fatal(err)
	}
	a.Markdown = "No image"
	a, err = savePostFixture(s, t.Context(), a)
	if err != nil {
		t.Fatal(err)
	}
	if public, err := s.MediaIsPublic(t.Context(), "/images/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.png"); err != nil || public {
		t.Fatal("historical image became public", public, err)
	}
	usage, err := s.ReferencedMedia(t.Context())
	if err != nil || len(usage["aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.png"].History) != 1 || usage["aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.png"].Current {
		t.Fatal(usage, err)
	}
	if err := s.DeleteRevision(t.Context(), "post", a.ID, old[0].ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow("SELECT count(*) FROM blog_post_media WHERE version_id=?", old[0].ID).Scan(&n); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	current, err := s.ListRevisions(t.Context(), "post", a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteRevision(t.Context(), "post", a.ID, current[0].ID); !errors.Is(err, errCurrentRevision) {
		t.Fatal(err)
	}
	if err := s.Delete(t.Context(), a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow("SELECT count(*) FROM blog_posts WHERE owner_id=?", a.ID).Scan(&n); err != nil || n != 0 {
		t.Fatal(n, err)
	}
}

func TestVersionPointerSaveRollback(t *testing.T) {
	for _, kind := range []string{"post", "page"} {
		t.Run(kind, func(t *testing.T) {
			h := newHarness(t)
			s := h.blog.posts.(*sqliteRepository)
			original := Revision{Title: "Original", Markdown: "![Image](/images/original.png)"}
			id, version, err := saveRevisionContent(t.Context(), s, kind, "", 0, original)
			if err != nil {
				t.Fatal(err)
			}
			current, _, err := revisionTable(kind)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.Exec("CREATE TRIGGER reject_pointer BEFORE UPDATE OF version_id ON " + current + " BEGIN SELECT RAISE(ABORT,'pointer failed'); END"); err != nil {
				t.Fatal(err)
			}
			before := schemaSnapshot(t, s.db)
			changed := Revision{Title: "Changed", Markdown: "![Image](/images/changed.png)"}
			if _, _, err := saveRevisionContent(t.Context(), s, kind, id, version, changed); err == nil {
				t.Fatal("failed pointer update reported success")
			}
			if schemaSnapshot(t, s.db) != before {
				t.Fatal("failed pointer update left partial settings, content, or references")
			}
			if kind == "post" {
				_, err = savePostFixture(s, t.Context(), Post{Slug: "new", Title: changed.Title, Markdown: changed.Markdown})
			} else {
				_, err = savePageFixture(s, t.Context(), Page{Slug: "new", Title: changed.Title, Markdown: changed.Markdown})
			}
			if err == nil || !strings.Contains(err.Error(), "pointer failed") {
				t.Fatal("creation did not reach the failing pointer update", err)
			}
			if schemaSnapshot(t, s.db) != before {
				t.Fatal("failed creation left partial content")
			}
		})
	}
}

// seedCurrentPost keeps direct SQL fixtures explicit without bypassing pointers.
func seedCurrentPost(ctx context.Context, tx *sql.Tx, p Post) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO blog_current_posts(id,version_id,slug,published,draft,version) VALUES(?,0,?,?,?,1)", p.ID, p.Slug, p.Published.Unix(), p.Draft)
	if err != nil {
		return err
	}
	n, err := saveContentVersion(ctx, tx, "post", p.ID, Revision{Title: p.Title, Summary: p.Summary, Markdown: p.Markdown, Tags: p.Tags}, nil)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE blog_current_posts SET version_id=? WHERE id=?", n, p.ID)
	return err
}

func seedCurrentPage(ctx context.Context, tx *sql.Tx, p Page) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO blog_current_pages(id,version_id,slug,published,parent_id,children_display,updated) VALUES(?,0,?,?,NULLIF(?,''),'hidden',?)", p.ID, p.Slug, p.Published, p.ParentID, p.Updated.UnixNano())
	if err != nil {
		return err
	}
	n, err := saveContentVersion(ctx, tx, "page", p.ID, Revision{Title: p.Title, Summary: p.Summary, Markdown: p.Markdown, Tags: p.Tags}, nil)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE blog_current_pages SET version_id=? WHERE id=?", n, p.ID)
	return err
}
