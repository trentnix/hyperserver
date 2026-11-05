package blog

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestSearchBodyCacheBounds(t *testing.T) {
	var cache searchBodyCache
	for i := range searchBodyEntries {
		cache.put(searchBody{key: searchBodyKey{"post", fmt.Sprint(i)}, version: 1})
	}
	first := searchBodyKey{"post", "0"}
	if text, ok := cache.get(first, 1); !ok || text != "" {
		t.Fatal("empty bodies must be cacheable")
	}
	cache.put(searchBody{key: searchBodyKey{"page", "0"}, version: 1, text: "page"})
	if _, ok := cache.get(searchBodyKey{"post", fmt.Sprint(searchBodyEntries - 2)}, 1); ok || cache.order.Len() != searchBodyEntries {
		t.Fatal("entry limit did not evict an unpromoted body")
	}
	if _, ok := cache.get(first, 2); ok {
		t.Fatal("cached a different saved version")
	}
	cache.put(searchBody{key: first, version: 2, text: "replacement"})
	if text, ok := cache.get(first, 2); !ok || text != "replacement" {
		t.Fatal("version replacement failed")
	}
	if text, ok := cache.get(searchBodyKey{"page", "0"}, 1); !ok || text != "page" {
		t.Fatal("post and page keys collided")
	}

	var bytesCache searchBodyCache
	body := strings.Repeat("x", searchBodyBytes/4-10)
	for i := range 5 {
		bytesCache.put(searchBody{key: searchBodyKey{"post", fmt.Sprint(i)}, version: 1, text: body})
	}
	if _, ok := bytesCache.get(searchBodyKey{"post", "3"}, 1); ok || bytesCache.bytes > searchBodyBytes || bytesCache.order.Len() != 4 {
		t.Fatal("byte limit did not evict an unpromoted body")
	}
	bytesCache.put(searchBody{key: first, version: 2, text: strings.Repeat("x", searchBodyBytes+1)})
	if _, ok := bytesCache.get(first, 2); ok || bytesCache.bytes > searchBodyBytes {
		t.Fatal("oversized body was retained")
	}
	var size int
	for e := bytesCache.order.Front(); e != nil; e = e.Next() {
		entry := e.Value.(searchBody)
		size += len(entry.key.id) + len(entry.text)
	}
	if size != bytesCache.bytes {
		t.Fatal("incorrect byte accounting")
	}
}

func TestSearchBodyCacheSequentialOverflow(t *testing.T) {
	for _, tc := range []struct {
		name    string
		count   int
		text    string
		minHits int
	}{
		{"entries", searchBodyEntries + 1, "small body", searchBodyEntries * 95 / 100},
		{"bytes", 5, strings.Repeat("x", searchBodyBytes/4-10), 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cache searchBodyCache
			for pass := range 3 {
				hits := 0
				for i := range tc.count {
					key := searchBodyKey{"post", fmt.Sprint(i)}
					if text, ok := cache.get(key, 1); ok {
						if text != tc.text {
							t.Fatal("incorrect cached body")
						}
						hits++
					} else {
						cache.put(searchBody{key: key, version: 1, text: tc.text})
					}
				}
				if pass > 0 && hits < tc.minHits {
					t.Fatalf("scan %d: %d cache hits, want at least %d", pass, hits, tc.minHits)
				}
				if cache.bytes > searchBodyBytes || cache.order.Len() > searchBodyEntries {
					t.Fatal("scan exceeded cache bounds")
				}
			}
		})
	}
}

func TestSearchBodyCacheAdaptsToNewContent(t *testing.T) {
	var cache searchBodyCache
	body := strings.Repeat("x", searchBodyBytes/4-10)
	for i := range 4 {
		cache.put(searchBody{key: searchBodyKey{"post", fmt.Sprint(i)}, version: 1, text: body})
	}
	// None of the original bodies are searched again. Repeated scans of new
	// content must eventually replace them, not keep a permanently pinned set.
	for range 4 * searchBodyPromotionInterval {
		for i := 4; i < 8; i++ {
			key := searchBodyKey{"post", fmt.Sprint(i)}
			if _, ok := cache.get(key, 1); !ok {
				cache.put(searchBody{key: key, version: 1, text: body})
			}
		}
	}
	for i := 4; i < 8; i++ {
		if _, ok := cache.get(searchBodyKey{"post", fmt.Sprint(i)}, 1); !ok {
			t.Fatal("unused bodies prevented caching the new working set")
		}
	}
}

func TestSearchBodyCacheConcurrent(t *testing.T) {
	var cache searchBodyCache
	var workers sync.WaitGroup
	for version := int64(1); version <= 8; version++ {
		workers.Go(func() {
			for i := range 100 {
				key := searchBodyKey{"post", fmt.Sprint(i)}
				text := fmt.Sprint(version)
				cache.put(searchBody{key: key, version: version, text: text})
				if got, ok := cache.get(key, version); ok && got != text {
					t.Error("cache returned another version's text")
				}
			}
		})
	}
	workers.Wait()
}

func TestSearchCacheEditsRestoresAndDeletes(t *testing.T) {
	for _, kind := range []string{"post", "page"} {
		t.Run(kind, func(t *testing.T) {
			h := newHarness(t)
			store := h.blog.posts.(*sqliteRepository)
			// A separate repository instance must invalidate the reader's cached
			// body through saved versions, not an in-process notification.
			writer := &sqliteRepository{db: store.db, now: store.now}
			content := Revision{Title: "Unrelated title", Markdown: "Old needle Straße e\u0301cole"}
			id, version, err := saveRevisionContent(t.Context(), writer, kind, "", 0, content)
			if err != nil {
				t.Fatal(err)
			}
			check := func(query string, count int) {
				t.Helper()
				got, err := store.SearchPublic(t.Context(), query)
				if err != nil || got.Total != count {
					t.Fatalf("public %q: %+v, %v", query, got, err)
				}
				if kind == "post" {
					got, err := store.SearchPosts(t.Context(), query)
					if err != nil || len(got) != count {
						t.Fatalf("posts %q: %+v, %v", query, got, err)
					}
				} else {
					got, err := store.SearchPages(t.Context(), query)
					if err != nil || len(got) != count {
						t.Fatalf("pages %q: %+v, %v", query, got, err)
					}
				}
			}
			check("old needle", 1)
			check("STRASSE", 1)
			check("ÉCOLE", 1)
			check("new needle", 0)
			content.Markdown = "New needle"
			_, version, err = saveRevisionContent(t.Context(), writer, kind, id, version, content)
			if err != nil {
				t.Fatal(err)
			}
			check("old needle", 0)
			check("new needle", 1)
			revisions, err := store.ListRevisions(t.Context(), kind, id)
			if err != nil {
				t.Fatal(err)
			}
			original, err := store.GetRevision(t.Context(), kind, id, revisions[len(revisions)-1].ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := saveRevisionContent(t.Context(), writer, kind, id, version, original); err != nil {
				t.Fatal(err)
			}
			check("old needle", 1)
			check("new needle", 0)
			if kind == "post" {
				err = writer.Delete(t.Context(), id)
			} else {
				err = writer.DeletePage(t.Context(), id)
			}
			if err != nil {
				t.Fatal(err)
			}
			check("old needle", 0)
		})
	}
}

func TestSearchCacheSnapshot(t *testing.T) {
	settings := Settings{}
	settings.Storage.Provider, settings.Storage.Connection = "sqlite", "posts.db?_journal=WAL"
	directory := t.TempDir()
	reader, closeReader, err := openRepository(t.Context(), settings, directory)
	if err != nil {
		t.Fatal(err)
	}
	defer closeReader()
	writer, closeWriter, err := openRepository(t.Context(), settings, directory)
	if err != nil {
		t.Fatal(err)
	}
	defer closeWriter()
	post, err := savePostFixture(writer, t.Context(), Post{Slug: "snapshot", Title: "Other", Markdown: "Old needle"})
	if err != nil {
		t.Fatal(err)
	}
	for _, warm := range []bool{false, true} {
		// Both a cold lookup and a cache hit must agree with the metadata snapshot,
		// even when another connection commits a newer version during the search.
		if warm {
			if _, err := reader.SearchPublic(t.Context(), "needle"); err != nil {
				t.Fatal(err)
			}
		}
		tx, err := reader.db.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		var version int64
		if err := tx.QueryRowContext(t.Context(), `SELECT version FROM blog_posts WHERE id=?`, post.ID).Scan(&version); err != nil {
			t.Fatal(err)
		}
		old := post.Markdown
		post.Markdown += " changed"
		post, err = savePostFixture(writer, t.Context(), post)
		if err != nil {
			t.Fatal(err)
		}
		if found, err := reader.matchBody(t.Context(), tx, newContentMatcher(post.Markdown), "post", post.ID, version); err != nil || found {
			t.Fatal("search mixed metadata and body versions", found, err)
		}
		if text, ok := reader.searchBodies.get(searchBodyKey{"post", post.ID}, version); !ok || text != strings.ToLower(old) {
			t.Fatal("cached body disagrees with its version", text, ok)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		got, err := reader.SearchPublic(t.Context(), post.Markdown)
		if err != nil || got.Total != 1 {
			t.Fatal("next search missed committed edit", got, err)
		}
	}
	post.Draft = true
	if _, err := savePostFixture(writer, t.Context(), post); err != nil {
		t.Fatal(err)
	}
	if got, err := reader.SearchPublic(t.Context(), "needle"); err != nil || got.Total != 0 {
		t.Fatal("cached draft leaked", got, err)
	}
}
