package blog

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestPostStoragePersistenceAndConflicts(t *testing.T) {
	settings := Settings{}
	settings.Storage.Provider, settings.Storage.Connection = "sqlite", "posts.db?_journal=WAL"
	directory := t.TempDir()
	store, closeStore, err := openRepository(t.Context(), settings, directory)
	if err != nil {
		t.Fatal(err)
	}
	post, err := store.Save(t.Context(), Post{Slug: "hello", Title: "Hello", Markdown: "World"})
	if err != nil {
		t.Fatal(err)
	}
	if err := closeStore(); err != nil {
		t.Fatal(err)
	}
	store, closeStore, err = openRepository(t.Context(), settings, directory)
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore()
	loaded, err := store.GetBySlug(t.Context(), "hello")
	if err != nil || loaded != post {
		t.Fatalf("post did not survive reopening: %v", err)
	}
	loaded.Title = "Changed"
	updated, err := store.Save(t.Context(), loaded)
	if err != nil || !updated.Published.Equal(post.Published) {
		t.Fatalf("edit changed publication time: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.List(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation ignored: %v", err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 4)
	for range 4 {
		wg.Go(func() {
			_, err := store.Save(t.Context(), Post{Slug: "one-winner", Title: "Concurrent", Markdown: "Post"})
			results <- err
		})
	}
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else if !errors.Is(err, errSlugTaken) {
			t.Errorf("unexpected conflict: %v", err)
		}
	}
	if winners != 1 {
		t.Fatalf("%d writes won", winners)
	}
	if err := store.Delete(t.Context(), post.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(t.Context(), post); !errors.Is(err, errNotFound) {
		t.Fatalf("edit recreated a deleted post: %v", err)
	}
}

func TestNavigationQueries(t *testing.T) {
	settings := Settings{}
	settings.Storage.Provider, settings.Storage.Connection = "sqlite", "posts.db"
	store, closeStore, err := openRepository(t.Context(), settings, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeStore() })
	if _, err := store.Latest(t.Context()); !errors.Is(err, errNotFound) {
		t.Fatalf("empty Latest: %v", err)
	}
	if entries, err := store.ListArchive(t.Context()); err != nil || len(entries) != 0 {
		t.Fatalf("empty archive: %v, %v", entries, err)
	}
	if entries, err := store.ListRecent(t.Context(), 3); err != nil || len(entries) != 0 {
		t.Fatalf("empty recent posts: %v, %v", entries, err)
	}

	// Dates and ties are explicit so the ordering check never depends on timing.
	date := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	db := store.(*sqliteRepository).db
	for index, post := range []Post{
		{ID: "first", Slug: "first", Title: "First", Published: date.AddDate(0, -1, 0)},
		{ID: "second", Slug: "second", Title: "Second", Published: date},
		{ID: "third", Slug: "third", Title: "Third", Published: date},
	} {
		_, err := db.ExecContext(t.Context(), "INSERT INTO blog_posts ("+postColumns+") VALUES (?, ?, ?, ?, ?, ?)", post.ID, post.Slug, post.Title, "Summary", "Body", post.Published.Unix())
		if err != nil {
			t.Fatalf("insert %d: %v", index, err)
		}
	}
	latest, err := store.Latest(t.Context())
	if err != nil || latest.ID != "third" || latest.Markdown != "Body" || latest.Summary != "Summary" {
		t.Fatalf("Latest = %+v, %v", latest, err)
	}
	entries, err := store.ListArchive(t.Context())
	want := []ArchiveEntry{{"third", "Third", date}, {"second", "Second", date}, {"first", "First", date.AddDate(0, -1, 0)}}
	if err != nil || !reflect.DeepEqual(entries, want) {
		t.Fatalf("archive = %+v, %v", entries, err)
	}
	for _, limit := range []int{1, 2, 3, 10} {
		recent, err := store.ListRecent(t.Context(), limit)
		if err != nil || !reflect.DeepEqual(recent, want[:min(limit, len(want))]) {
			t.Fatalf("recent limit=%d: %+v, %v", limit, recent, err)
		}
	}
	for _, limit := range []int{0, -1} {
		if _, err := store.ListRecent(t.Context(), limit); err == nil {
			t.Fatalf("accepted invalid recent post limit %d", limit)
		}
	}
	months := groupArchive(entries)
	if len(months) != 2 || months[0].Name != "October 2026" || len(months[0].Posts) != 2 || months[1].Name != "September 2026" {
		t.Fatalf("grouped archive = %+v", months)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.Latest(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Latest ignored cancellation: %v", err)
	}
	if _, err := store.ListArchive(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("ListArchive ignored cancellation: %v", err)
	}
	if _, err := store.ListRecent(ctx, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("ListRecent ignored cancellation: %v", err)
	}
	if err := closeStore(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Latest(t.Context()); err == nil || errors.Is(err, errNotFound) {
		t.Fatalf("Latest hid storage failure: %v", err)
	}
	if _, err := store.ListArchive(t.Context()); err == nil {
		t.Fatal("ListArchive hid storage failure")
	}
	if _, err := store.ListRecent(t.Context(), 1); err == nil {
		t.Fatal("ListRecent hid storage failure")
	}
}

func TestPostValidation(t *testing.T) {
	base := Post{Title: "Hello", Markdown: "World"}
	if message := validatePost(base); message != "" {
		t.Fatal(message)
	}
	for _, slug := range []string{"../path", "UPPER", "a--b", "-start", "end-", "two words", "é"} {
		post := base
		post.Slug = slug
		if validatePost(post) == "" {
			t.Errorf("accepted slug %q", slug)
		}
	}
	for _, field := range []string{"title", "summary", "markdown"} {
		post := base
		switch field {
		case "title":
			post.Title = "\xff"
		case "summary":
			post.Summary = "\xff"
		case "markdown":
			post.Markdown = "\xff"
		}
		if validatePost(post) == "" {
			t.Errorf("accepted invalid UTF-8 in %s", field)
		}
	}
	post := base
	post.ID = "existing"
	if validatePost(post) == "" {
		t.Fatal("accepted missing slug on edit")
	}
}
