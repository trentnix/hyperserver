package blog

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
)

// saveRevisionContent exercises the same content contract for posts and pages.
func saveRevisionContent(ctx context.Context, store *sqliteRepository, kind, id string, version int64, content Revision) (string, int64, error) {
	if kind == "post" {
		p, err := store.Save(ctx, Post{ID: id, Version: version, Slug: "history", Title: content.Title, Summary: content.Summary, Markdown: content.Markdown, PreviewImage: content.PreviewImage})
		return p.ID, p.Version, err
	}
	p, err := store.SavePage(ctx, Page{ID: id, Version: version, Slug: "history", Title: content.Title, Summary: content.Summary, Markdown: content.Markdown, PreviewImage: content.PreviewImage, Published: true})
	return p.ID, p.Version, err
}

// TestRevisionStorage covers immutable snapshots, no-op saves, settings-only
// changes, restoring old writing, owner isolation, and deletion.
func TestRevisionStorage(t *testing.T) {
	for _, kind := range []string{"post", "page"} {
		t.Run(kind, func(t *testing.T) {
			h := newHarness(t)
			store := h.blog.posts.(*sqliteRepository)
			original := Revision{Title: "Original", Summary: "Summary", Markdown: "First body", PreviewImage: "original.png"}
			id, version, err := saveRevisionContent(t.Context(), store, kind, "", 0, original)
			if err != nil || version != 1 {
				t.Fatal("create", version, err)
			}
			versions := func(want int) []Revision {
				t.Helper()
				list, err := store.ListRevisions(t.Context(), kind, id)
				if err != nil || len(list) != want {
					t.Fatalf("versions=%v, want %d: %v", list, want, err)
				}
				available, err := store.HasHistory(t.Context(), kind, id)
				if err != nil || available != (want > 1) {
					t.Fatal("incorrect history availability", available, err)
				}
				owners, err := store.HistoryOwners(t.Context(), kind)
				if err != nil || owners[id] != (want > 1) {
					t.Fatal("incorrect management history availability", owners, err)
				}
				for i, r := range list {
					if r.Markdown != "" || r.RecordedAt.IsZero() || (i > 0 && r.ID >= list[i-1].ID) {
						t.Fatal("history must be newest-first metadata", list)
					}
				}
				return list
			}
			first := versions(1)[0].ID
			_, version, err = saveRevisionContent(t.Context(), store, kind, id, version, original)
			if err != nil || version != 2 {
				t.Fatal("unchanged save", version, err)
			}
			versions(1)

			if kind == "post" {
				p, err := store.GetByID(t.Context(), id, true)
				if err != nil {
					t.Fatal(err)
				}
				p.Slug, p.Draft = "new-url", true
				p, err = store.Save(t.Context(), p)
				if err != nil {
					t.Fatal(err)
				}
				version = p.Version
			} else {
				p, err := store.GetPageByID(t.Context(), id, true)
				if err != nil {
					t.Fatal(err)
				}
				p.Slug, p.Published, p.ParentID, p.ChildrenDisplay, p.DisplayOrder = "new-url", false, h.projectsID, "blocks", new(7)
				p, err = store.SavePage(t.Context(), p)
				if err != nil {
					t.Fatal(err)
				}
				version = p.Version
			}
			versions(1)

			current := original
			for _, change := range []func(*Revision){
				func(r *Revision) { r.Title = "New title" },
				func(r *Revision) { r.Summary = "New summary" },
				func(r *Revision) { r.Markdown = "New body" },
				func(r *Revision) { r.PreviewImage = "new.png" },
			} {
				change(&current)
				_, version, err = saveRevisionContent(t.Context(), store, kind, id, version, current)
				if err != nil {
					t.Fatal(err)
				}
			}
			versions(5)
			stored, err := store.GetRevision(t.Context(), kind, id, first)
			if err != nil {
				t.Fatal(err)
			}
			stored.ID, stored.RecordedAt = 0, original.RecordedAt
			if stored != original {
				t.Fatal("later saves changed the original snapshot", stored)
			}
			_, _, err = saveRevisionContent(t.Context(), store, kind, id, version, original)
			if err != nil {
				t.Fatal(err)
			}
			versions(6)
			if _, err := store.GetRevision(t.Context(), kind, "another-owner", first); !errors.Is(err, errNotFound) {
				t.Fatal("cross-owner lookup", err)
			}
			if kind == "post" {
				err = store.Delete(t.Context(), id)
			} else {
				err = store.DeletePage(t.Context(), id)
			}
			if err != nil {
				t.Fatal(err)
			}
			versions(0)
			if _, err := store.GetRevision(t.Context(), kind, id, first); !errors.Is(err, errNotFound) {
				t.Fatal("deleted owner's history survived", err)
			}
		})
	}
}

// TestRevisionSaveAtomicity rejects stale tabs and rolls back content, image
// references, and edit versions when recording the snapshot fails.
func TestRevisionSaveAtomicity(t *testing.T) {
	for _, kind := range []string{"post", "page"} {
		t.Run(kind, func(t *testing.T) {
			store := newHarness(t).blog.posts.(*sqliteRepository)
			store.db.SetMaxOpenConns(3)
			original := Revision{Title: "Original", Markdown: "Body", PreviewImage: "original.png"}
			id, version, err := saveRevisionContent(t.Context(), store, kind, "", 0, original)
			if err != nil {
				t.Fatal(err)
			}
			_, table, _ := revisionTable(kind)
			if _, err := store.db.Exec("CREATE TRIGGER fail_revision BEFORE INSERT ON " + table + " BEGIN SELECT RAISE(ABORT, 'snapshot failure'); END"); err != nil {
				t.Fatal(err)
			}
			if kind == "post" {
				if _, err := store.Save(t.Context(), Post{Slug: "uncommitted", Title: "Failed create"}); err == nil {
					t.Fatal("failed snapshot accepted a new post")
				}
				if _, err := store.GetBySlug(t.Context(), "uncommitted", true); !errors.Is(err, errNotFound) {
					t.Fatal("failed create left a post behind", err)
				}
			} else {
				if _, err := store.SavePage(t.Context(), Page{Slug: "uncommitted", Title: "Failed create"}); err == nil {
					t.Fatal("failed snapshot accepted a new page")
				}
				if _, err := store.GetPageBySlug(t.Context(), "uncommitted", true); !errors.Is(err, errNotFound) {
					t.Fatal("failed create left a page behind", err)
				}
			}
			changed := Revision{Title: "Changed", Markdown: "New body", PreviewImage: "new.png"}
			if _, _, err := saveRevisionContent(t.Context(), store, kind, id, version, changed); err == nil {
				t.Fatal("snapshot failure accepted a save")
			}
			for ref, want := range map[string]bool{"original.png": true, "new.png": false} {
				if public, err := store.ImageIsPublic(t.Context(), "/images/"+ref); err != nil || public != want {
					t.Fatal("failed save changed image access", public, err)
				}
			}
			if _, err := store.db.Exec("DROP TRIGGER fail_revision"); err != nil {
				t.Fatal(err)
			}

			results := make(chan error, 8)
			var writers sync.WaitGroup
			for range 8 {
				writers.Go(func() {
					_, _, err := saveRevisionContent(t.Context(), store, kind, id, version, changed)
					results <- err
				})
			}
			writers.Wait()
			close(results)
			winners := 0
			for err := range results {
				if err == nil {
					winners++
				} else if !errors.Is(err, errEditConflict) {
					t.Fatal(err)
				}
			}
			if winners != 1 {
				t.Fatal("concurrent saves must have exactly one winner", winners)
			}
			if list, err := store.ListRevisions(t.Context(), kind, id); err != nil || len(list) != 2 {
				t.Fatal("failed saves recorded history", list, err)
			}
			if _, _, err := saveRevisionContent(t.Context(), store, kind, "missing", version, changed); !errors.Is(err, errNotFound) {
				t.Fatal("missing content must not be reported as stale", err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if _, _, err := saveRevisionContent(ctx, store, kind, id, version+1, changed); !errors.Is(err, context.Canceled) {
				t.Fatal("save ignored cancellation", err)
			}
			if _, err := store.ListRevisions(ctx, kind, id); !errors.Is(err, context.Canceled) {
				t.Fatal("history ignored cancellation", err)
			}
			if _, err := store.GetRevision(ctx, kind, id, 1); !errors.Is(err, context.Canceled) {
				t.Fatal("snapshot lookup ignored cancellation", err)
			}
			if _, err := store.HasHistory(ctx, kind, id); !errors.Is(err, context.Canceled) {
				t.Fatal("history availability ignored cancellation", err)
			}
			if _, err := store.HistoryOwners(ctx, kind); !errors.Is(err, context.Canceled) {
				t.Fatal("history owners ignored cancellation", err)
			}
		})
	}
}

// TestPublicationInvalidatesEditor prevents an old editor from silently undoing
// a publication shortcut used in another tab. Visibility changes do not add snapshots.
func TestPublicationInvalidatesEditor(t *testing.T) {
	store := newHarness(t).blog.posts.(*sqliteRepository)
	p, err := store.Save(t.Context(), Post{Slug: "publication", Title: "Original", Markdown: "Body"})
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.SetPublished(t.Context(), p.ID, false)
	if err != nil || current.Version != p.Version+1 {
		t.Fatal("publication did not invalidate old editors", current, err)
	}
	p.Title = "Stale edit"
	if _, err := store.Save(t.Context(), p); !errors.Is(err, errEditConflict) {
		t.Fatal("stale editor reversed publication", err)
	}
	stored, err := store.GetByID(t.Context(), p.ID, true)
	if err != nil || stored != current {
		t.Fatal("rejected edit changed storage", stored, err)
	}
	if versions, err := store.ListRevisions(t.Context(), "post", p.ID); err != nil || len(versions) != 1 {
		t.Fatal("publication or stale edit created a snapshot", versions, err)
	}
}

// TestRevisionBackfill verifies atomic baseline capture and repeatable startup.
func TestRevisionBackfill(t *testing.T) {
	store := newHarness(t).blog.posts.(*sqliteRepository)
	id, _, err := saveRevisionContent(t.Context(), store, "post", "", 0, Revision{Title: "Existing", Markdown: "Body"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`DROP TABLE blog_post_revisions; DROP TABLE blog_page_revisions;
ALTER TABLE blog_posts DROP COLUMN version; ALTER TABLE blog_pages DROP COLUMN version;
CREATE TABLE blog_page_revisions_owner (id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if err := initializeRevisions(t.Context(), store.db); err == nil {
		t.Fatal("backfill accepted conflicting schema")
	}
	var count int
	if err := store.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name IN ('blog_post_revisions', 'blog_page_revisions')").Scan(&count); err != nil || count != 0 {
		t.Fatal("partial history tables survived failure", count, err)
	}
	if err := store.db.QueryRow("SELECT count(*) FROM pragma_table_info('blog_posts') WHERE name='version'").Scan(&count); err != nil || count != 0 {
		t.Fatal("edit-version column survived failed upgrade", count, err)
	}
	if _, err := store.db.Exec("DROP TABLE blog_page_revisions_owner"); err != nil {
		t.Fatal(err)
	}
	if err := initializeRevisions(t.Context(), store.db); err != nil {
		t.Fatal(err)
	}
	before, err := store.ListRevisions(t.Context(), "post", id)
	if err != nil || len(before) != 1 || before[0].Title != "Existing" {
		t.Fatal("existing post was not captured", before, err)
	}
	var pages, snapshots int
	if err := store.db.QueryRow("SELECT (SELECT count(*) FROM blog_pages), (SELECT count(*) FROM blog_page_revisions)").Scan(&pages, &snapshots); err != nil || pages != snapshots {
		t.Fatal("existing pages were not captured", pages, snapshots, err)
	}
	if err := initializeRevisions(t.Context(), store.db); err != nil {
		t.Fatal(err)
	}
	after, err := store.ListRevisions(t.Context(), "post", id)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("reopening changed baseline history", after, err)
	}
	if _, err := store.ListRevisions(t.Context(), "invalid", id); !errors.Is(err, errNotFound) {
		t.Fatal("invalid content type accepted", err)
	}
	if _, err := store.HasHistory(t.Context(), "invalid", id); !errors.Is(err, errNotFound) {
		t.Fatal("history availability accepted invalid content type", err)
	}
	if _, err := store.HistoryOwners(t.Context(), "invalid"); !errors.Is(err, errNotFound) {
		t.Fatal("history owners accepted invalid content type", err)
	}
	if available, err := store.HasHistory(t.Context(), "post", "missing"); err != nil || available {
		t.Fatal("missing content has history", available, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := initializeRevisions(ctx, store.db); !errors.Is(err, context.Canceled) {
		t.Fatal("startup ignored cancellation", err)
	}
}
