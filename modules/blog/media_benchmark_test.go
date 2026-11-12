package blog

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// BenchmarkMediaLookups measures repository work on warm SQLite files. Each
// upload has metadata, three post revisions, and a current reference. Image
// bytes, filesystem enumeration, HTTP rendering, and authentication are excluded.
func BenchmarkMediaLookups(b *testing.B) {
	for _, count := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("images=%d", count), func(b *testing.B) {
			settings := Settings{}
			settings.Storage.Provider, settings.Storage.Connection = "sqlite", "media.db"
			store, closeStore, err := openRepository(b.Context(), settings, b.TempDir())
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() {
				if err := closeStore(); err != nil {
					b.Error(err)
				}
			})
			tx, err := store.db.BeginTx(b.Context(), nil)
			if err != nil {
				b.Fatal(err)
			}
			defer tx.Rollback()
			var ids []string
			for i := range count {
				id := fmt.Sprintf("%064x.png", i)
				ids = append(ids, id)
				if _, err := tx.ExecContext(b.Context(), `INSERT INTO blog_media(id,filename,label,tags,version) VALUES(?,?,?,'forest, travel',1)`, id, fmt.Sprintf("photo-%d.png", i), "A walk in the forest"); err != nil {
					b.Fatal(err)
				}
				post := Post{ID: fmt.Sprintf("post-%d", i), Slug: fmt.Sprintf("post-%d", i), Title: "A walk in the forest", Markdown: strings.Repeat("Saved writing. ", 100), Published: time.Now()}
				if err := seedCurrentPost(b.Context(), tx, post); err != nil {
					b.Fatal(err)
				}
				if _, err := tx.ExecContext(b.Context(), `INSERT INTO blog_post_media(version_id,url) SELECT version_id,? FROM blog_current_posts WHERE id=?`, "/images/"+id, post.ID); err != nil {
					b.Fatal(err)
				}
				for revision := range 2 {
					result, err := tx.ExecContext(b.Context(), `INSERT INTO blog_posts(owner_id,title,summary,markdown,preview_image,recorded,content_path) VALUES(?,?,'',?,'',?,'/post/s/fixture')`, post.ID, post.Title, post.Markdown, int64(revision))
					if err != nil {
						b.Fatal(err)
					}
					version, err := result.LastInsertId()
					if err != nil {
						b.Fatal(err)
					}
					if _, err := tx.ExecContext(b.Context(), `INSERT INTO blog_post_media(version_id,url) VALUES(?,?)`, version, "//old.example/images/"+id); err != nil {
						b.Fatal(err)
					}
				}
			}
			if err := tx.Commit(); err != nil {
				b.Fatal(err)
			}
			for _, selected := range []struct {
				name string
				ids  []string
			}{{"all", nil}, {"one", ids[:1]}, {"page", ids[:mediaPageSize]}} {
				expectedIDs := ids
				if len(selected.ids) > 0 {
					expectedIDs = selected.ids
				}
				expected := len(expectedIDs)
				b.Run("usage/"+selected.name, func(b *testing.B) {
					// Validate and warm the lookup before measuring it. An empty
					// or incomplete result must not look like an optimization.
					usage, err := store.ReferencedMedia(b.Context(), selected.ids...)
					if err != nil || len(usage) != expected {
						b.Fatalf("usage: got %d images, want %d: %v", len(usage), expected, err)
					}
					for _, id := range expectedIDs {
						value := usage[id]
						if !value.Current || len(value.Content) != 1 || len(value.History) != 3 {
							b.Fatalf("incomplete usage for %s: %+v", id, value)
						}
					}
					b.ReportAllocs()
					for b.Loop() {
						usage, err := store.ReferencedMedia(b.Context(), selected.ids...)
						if err != nil || len(usage) != expected {
							b.Fatalf("usage: got %d images, want %d: %v", len(usage), expected, err)
						}
					}
				})
				b.Run("metadata/"+selected.name, func(b *testing.B) {
					metadata, err := store.ListMediaMetadata(b.Context(), selected.ids...)
					if err != nil || len(metadata) != expected {
						b.Fatalf("metadata: got %d images, want %d: %v", len(metadata), expected, err)
					}
					for _, id := range expectedIDs {
						value := metadata[id]
						if value.Filename == "" || value.Label != "A walk in the forest" || value.Tags != "forest, travel" || value.Version != 1 {
							b.Fatalf("incomplete metadata for %s: %+v", id, value)
						}
					}
					b.ReportAllocs()
					for b.Loop() {
						metadata, err := store.ListMediaMetadata(b.Context(), selected.ids...)
						if err != nil || len(metadata) != expected {
							b.Fatalf("metadata: got %d images, want %d: %v", len(metadata), expected, err)
						}
					}
				})
			}
		})
	}
}
