package blog

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// TestImageReferenceDeduplication ensures repeated uses and a preview selection
// share one reference without causing a failed save or losing publication access.
func TestImageReferenceDeduplication(t *testing.T) {
	h := newHarness(t)
	store := h.blog.posts.(*sqliteRepository)
	p, err := store.Save(t.Context(), Post{Slug: "repeated", Title: "Repeated", PreviewImage: "same.png",
		Markdown: "![One](/images/same.png)\n\n![Two](/images/same.png?size=2)\n\n[Download](/images/same.png#download)"})
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.db.QueryRowContext(t.Context(), "SELECT count(*) FROM blog_post_images WHERE owner_id=?", p.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("references were not deduplicated", count, err)
	}
	p.PreviewImage = ""
	p.Markdown = "![Remaining](/images/same.png)"
	if _, err := store.Save(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	if public, err := store.ImageIsPublic(t.Context(), "/images/same.png"); err != nil || !public {
		t.Fatal("removing a duplicate revoked the remaining reference", public, err)
	}
	if public, err := store.ImageIsPublic(t.Context()); err != nil || public {
		t.Fatal("empty lookup granted access", public, err)
	}
}

// TestImageReferencesConcurrentSave never grants access to an image associated
// only with draft versions, even as the same owner repeatedly changes visibility.
func TestImageReferencesConcurrentSave(t *testing.T) {
	h := newHarness(t)
	store := h.blog.posts.(*sqliteRepository)
	// Separate connections let the reader run during a write transaction.
	store.db.SetMaxOpenConns(3)
	for _, kind := range []string{"post", "page"} {
		t.Run(kind, func(t *testing.T) {
			var id string
			save := func(draft bool) error {
				preview := "public.png"
				if draft {
					preview = "private.png"
				}
				if kind == "post" {
					p, err := store.Save(t.Context(), Post{ID: id, Slug: "concurrent-post", Title: "Post", PreviewImage: preview, Draft: draft})
					if err == nil {
						id = p.ID
					}
					return err
				}
				p, err := store.SavePage(t.Context(), Page{ID: id, Slug: "concurrent-page", Title: "Page", PreviewImage: preview, Published: !draft})
				if err == nil {
					id = p.ID
				}
				return err
			}
			if err := save(false); err != nil {
				t.Fatal(err)
			}
			var wg sync.WaitGroup
			start := make(chan struct{})
			wg.Go(func() {
				<-start
				for i := range 100 {
					if err := save(i%2 == 0); err != nil {
						t.Error(err)
						return
					}
				}
			})
			wg.Go(func() {
				<-start
				for range 200 {
					if public, err := store.ImageIsPublic(t.Context(), "/images/private.png"); err != nil || public {
						t.Error("read saw private references as public", public, err)
						return
					}
				}
			})
			close(start)
			wg.Wait()
		})
	}
}

// TestImageReferenceBackfillFailure rolls back both tables when the second table
// cannot initialize. A later successful attempt must rebuild all references.
func TestImageReferenceBackfillFailure(t *testing.T) {
	h := newHarness(t)
	store := h.blog.posts.(*sqliteRepository)
	if _, err := store.Save(t.Context(), Post{Slug: "existing", Title: "Existing", PreviewImage: "existing.png"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(t.Context(), `DROP TABLE blog_post_images; DROP TABLE blog_page_images;
CREATE TABLE blog_page_images_url (id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if err := initializeImageReferences(t.Context(), store.db); err == nil {
		t.Fatal("backfill accepted a conflicting index name")
	}
	var count int
	if err := store.db.QueryRowContext(t.Context(), "SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('blog_post_images', 'blog_page_images')").Scan(&count); err != nil || count != 0 {
		t.Fatal("failed backfill left partially initialized tables", count, err)
	}
	if _, err := store.db.ExecContext(t.Context(), "DROP TABLE blog_page_images_url"); err != nil {
		t.Fatal(err)
	}
	if err := initializeImageReferences(t.Context(), store.db); err != nil {
		t.Fatal(err)
	}
	if public, err := store.ImageIsPublic(t.Context(), "/images/existing.png"); err != nil || !public {
		t.Fatal("retry did not restore existing references", public, err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := initializeImageReferences(ctx, store.db); !errors.Is(err, context.Canceled) {
		t.Fatal("backfill ignored cancellation", err)
	}
	if _, err := store.ImageIsPublic(ctx, "/images/existing.png"); !errors.Is(err, context.Canceled) {
		t.Fatal("lookup ignored cancellation", err)
	}
}
