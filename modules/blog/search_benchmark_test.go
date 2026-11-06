package blog

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// BenchmarkSearch measures repository work and pool contention, not HTTP,
// rendering, or per-client admission. The public search deadline still applies.
func BenchmarkSearch(b *testing.B) {
	for _, size := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("posts=%d", size), func(b *testing.B) {
			store := searchBenchmarkStore(b, size)
			for _, query := range []struct{ name, text string }{
				{"title", "journal"}, {"body", "observatory"}, {"unicode", "STRASSE"}, {"miss", "never-present-token"},
			} {
				for _, workers := range []int{1, 4, 16} {
					if workers != 1 && query.name != "miss" {
						continue
					}
					b.Run(fmt.Sprintf("public/%s/workers=%d", query.name, workers), func(b *testing.B) {
						searchBenchmark(b, store, workers, true, func(ctx context.Context) error {
							results, err := store.SearchPublic(ctx, query.text)
							if err == nil && (len(results.Items) > searchResultLimit || (query.name == "miss" && results.Total != 0) || (query.name != "miss" && results.Total == 0)) {
								return fmt.Errorf("unexpected search results: %d", results.Total)
							}
							return err
						})
					})
				}
			}
			b.Run("admin/posts/body", func(b *testing.B) {
				searchBenchmark(b, store, 1, false, func(ctx context.Context) error {
					_, err := store.SearchPosts(ctx, "observatory")
					return err
				})
			})
			b.Run("admin/pages/body", func(b *testing.B) {
				searchBenchmark(b, store, 1, false, func(ctx context.Context) error {
					_, err := store.SearchPages(ctx, "observatory")
					return err
				})
			})
			// Measure first-search work separately. The regular cases retain the
			// bounded body cache, but never cache results or query strings.
			b.Run("cold/miss", func(b *testing.B) {
				searchBenchmark(b, store, 1, true, func(ctx context.Context) error {
					store.searchBodies.mu.Lock()
					store.searchBodies.entries = nil
					store.searchBodies.order.Init()
					store.searchBodies.bytes = 0
					store.searchBodies.inserts = 0
					store.searchBodies.mu.Unlock()
					_, err := store.SearchPublic(ctx, "never-present-token")
					return err
				})
			})
		})
	}
}

// BenchmarkSearchOverflow exceeds the body-cache budget even for public content.
// Repeated scans must still benefit from the portion that fits in the cache.
func BenchmarkSearchOverflow(b *testing.B) {
	store := searchBenchmarkStore(b, 20000)
	searchBenchmark(b, store, 1, true, func(ctx context.Context) error {
		results, err := store.SearchPublic(ctx, "never-present-token")
		if err == nil && results.Total != 0 {
			return fmt.Errorf("unexpected matches: %d", results.Total)
		}
		return err
	})
}

func searchBenchmark(b *testing.B, store *sqliteRepository, workers int, deadline bool, search func(context.Context) error) {
	// Warm SQLite and filesystem caches before measuring steady-state searches.
	if err := search(b.Context()); err != nil {
		b.Fatal(err)
	}
	latencies := make([]time.Duration, b.N)
	var next, timeouts atomic.Int64
	var group sync.WaitGroup
	before := store.db.Stats()
	b.ReportAllocs()
	b.ResetTimer()
	for range workers {
		group.Go(func() {
			for {
				i := int(next.Add(1) - 1)
				if i >= b.N {
					return
				}
				start := time.Now()
				ctx := b.Context()
				var cancel context.CancelFunc
				if deadline {
					ctx, cancel = context.WithTimeout(ctx, searchTimeout)
				}
				err := search(ctx)
				if cancel != nil {
					cancel()
				}
				latencies[i] = time.Since(start)
				if errors.Is(err, context.DeadlineExceeded) {
					timeouts.Add(1)
				} else if err != nil {
					b.Error(err)
					return
				}
			}
		})
	}
	group.Wait()
	b.StopTimer()
	after := store.db.Stats()
	slices.Sort(latencies)
	for _, percentile := range []int{50, 95, 99} {
		index := (len(latencies)*percentile+99)/100 - 1
		b.ReportMetric(float64(latencies[index])/float64(time.Millisecond), fmt.Sprintf("p%d-ms", percentile))
	}
	b.ReportMetric(float64(after.WaitDuration-before.WaitDuration)/float64(b.N)/float64(time.Millisecond), "pool-wait-ms/op")
	b.ReportMetric(float64(timeouts.Load())*100/float64(b.N), "timeout-%")
	b.ReportMetric(float64(store.searchBodies.bytes)/(1<<20), "cached-MiB")
}

func searchBenchmarkStore(b *testing.B, posts int) *sqliteRepository {
	settings := Settings{}
	settings.Storage.Provider, settings.Storage.Connection = "sqlite", "search.db?_journal=WAL"
	store, closeStore, err := openRepository(b.Context(), settings, b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := closeStore(); err != nil {
			b.Error(err)
		}
	})
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	tx, err := store.db.BeginTx(b.Context(), nil)
	if err != nil {
		b.Fatal(err)
	}
	defer tx.Rollback()
	revision, err := tx.PrepareContext(b.Context(), `INSERT INTO blog_posts(owner_id,title,summary,markdown,preview_image,recorded,content_path) VALUES(?,?,?,?,'',?,'/post/s/fixture')`)
	if err != nil {
		b.Fatal(err)
	}
	defer revision.Close()
	paragraph := "A walk through the city, notes on building software, and lessons from a weekend project. Café discussions and thoughtful testing help explain the design.\n\n"
	for i := range posts {
		id := fmt.Sprintf("post-%06d", i)
		body := strings.Repeat(paragraph, 8+i%40)
		if i%20 == 1 {
			body += "\nA visit to the observatory on the Straße.\n"
		}
		title, summary := fmt.Sprintf("Journal entry %06d", i), "Notes on a project, a walk, and what I learned."
		published := now.Add(-time.Duration(i+1) * time.Hour)
		if i%20 == 2 {
			published = now.Add(time.Hour)
		}
		if err := seedCurrentPost(b.Context(), tx, Post{ID: id, Slug: id, Title: title, Summary: summary, Markdown: body, Published: published, Draft: i%10 == 0, Tags: "journal, software"}); err != nil {
			b.Fatal(err)
		}
		for version := range 2 {
			if _, err := revision.ExecContext(b.Context(), id, title, summary, body, now.Add(-time.Duration(version+1)*time.Hour).UnixNano()); err != nil {
				b.Fatal(err)
			}
		}
		if i < posts/10 {
			pageID := fmt.Sprintf("page-%06d", i)
			var parent string
			if i%5 != 0 {
				parent = fmt.Sprintf("page-%06d", i-i%5)
			}
			if err := seedCurrentPage(b.Context(), tx, Page{ID: pageID, Slug: pageID, Title: title, Summary: summary, Markdown: body, Published: i%10 != 0, ParentID: parent, Updated: now, Tags: "journal, software"}); err != nil {
				b.Fatal(err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	if _, err := store.db.ExecContext(b.Context(), `PRAGMA wal_checkpoint(TRUNCATE); ANALYZE`); err != nil {
		b.Fatal(err)
	}
	return store
}
