package blog

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestImageReferenceCancellation(t *testing.T) {
	store := newHarness(t).blog.posts.(*sqliteRepository)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.MediaIsPublic(ctx, "/images/example.png"); !errors.Is(err, context.Canceled) {
		t.Fatal("lookup ignored cancellation", err)
	}
}

// TestImageReferenceDeduplication ensures repeated uses and a preview selection
// share one reference without causing a failed save or losing publication access.
func TestImageReferenceDeduplication(t *testing.T) {
	h := newHarness(t)
	store := h.blog.posts.(*sqliteRepository)
	p, err := savePostFixture(store, t.Context(), Post{Slug: "repeated", Title: "Repeated", PreviewImage: "same.png",
		Markdown: "![One](/images/same.png)\n\n![Two](/images/same.png?size=2)\n\n[Download](/images/same.png#download)"})
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.db.QueryRowContext(t.Context(), "SELECT count(*) FROM blog_post_media WHERE version_id=(SELECT version_id FROM blog_current_posts WHERE id=?)", p.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("references were not deduplicated", count, err)
	}
	p.PreviewImage = ""
	p.Markdown = "![Remaining](/images/same.png)"
	if _, err := savePostFixture(store, t.Context(), p); err != nil {
		t.Fatal(err)
	}
	if public, err := store.MediaIsPublic(t.Context(), "/images/same.png"); err != nil || !public {
		t.Fatal("removing a duplicate revoked the remaining reference", public, err)
	}
	if public, err := store.MediaIsPublic(t.Context()); err != nil || public {
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
			var version int64
			save := func(draft bool) error {
				preview := "public.png"
				if draft {
					preview = "private.png"
				}
				if kind == "post" {
					p, err := savePostFixture(store, t.Context(), Post{ID: id, Version: version, Slug: "concurrent-post", Title: "Post", PreviewImage: preview, Draft: draft})
					if err == nil {
						id, version = p.ID, p.Version
					}
					return err
				}
				p, err := savePageFixture(store, t.Context(), Page{ID: id, Version: version, Slug: "concurrent-page", Title: "Page", PreviewImage: preview, Published: !draft})
				if err == nil {
					id, version = p.ID, p.Version
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
					if public, err := store.MediaIsPublic(t.Context(), "/images/private.png"); err != nil || public {
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
