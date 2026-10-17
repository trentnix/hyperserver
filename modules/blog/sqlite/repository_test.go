package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/trentnix/hyperserver/modules/blog"
	databasesqlite "github.com/trentnix/hyperserver/pkg/database/sqlite"
)

func openTest(t *testing.T) (*Repository, *sql.DB) {
	t.Helper()
	db, err := databasesqlite.Open(filepath.Join(t.TempDir(), "blog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	s := New(db)
	if err := s.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	return s, db
}

func TestStoredBoundsAndInitializationRollback(t *testing.T) {
	s, db := openTest(t)
	ctx := t.Context()
	p := testPost("version-limit")
	if err := s.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE blog_posts SET version=? WHERE id=?", int64(math.MaxInt64), p.ID); err != nil {
		t.Fatal(err)
	}
	p.Version = math.MaxInt64
	if err := s.Update(ctx, p); !errors.Is(err, blog.ErrConflict) {
		t.Fatal("version overflow", err)
	}
	if got, err := s.GetByID(ctx, p.ID); err != nil || got.Version != math.MaxInt64 {
		t.Fatal("version changed", got, err)
	}
	imageID := uuid.NewString()
	if _, err := db.ExecContext(ctx, "INSERT INTO blog_images(id,media_type,data) VALUES (?,'image/png',zeroblob(?))", imageID, blog.MaxImageBytes+1); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Get(ctx, imageID); err == nil || len(got.Data) != 0 {
		t.Fatal("oversized stored image loaded", err)
	}

	broken, err := databasesqlite.Open(filepath.Join(t.TempDir(), "broken.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer broken.Close()
	if _, err := broken.ExecContext(ctx, "CREATE TABLE blog_images (id TEXT)"); err != nil {
		t.Fatal(err)
	}
	if err := New(broken).Initialize(ctx); err == nil {
		t.Fatal("incompatible schema accepted")
	}
	var count int
	if err := broken.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE name='blog_posts'").Scan(&count); err != nil || count != 0 {
		t.Fatal("partial schema was committed", count, err)
	}
}

func testPost(slug string) blog.Post {
	return blog.Post{ID: uuid.NewString(), Slug: slug, Title: "Post", Summary: "Summary", Markdown: "Hello **world**", PublishedAt: time.Now().UTC(), Version: 1}
}

func TestRepositoryLifecycle(t *testing.T) {
	s, db := openTest(t)
	ctx := t.Context()
	p := testPost("hello")
	if err := s.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	for _, read := range []func(context.Context, string) (blog.Post, error){s.GetByID, func(ctx context.Context, _ string) (blog.Post, error) { return s.GetBySlug(ctx, p.Slug) }} {
		got, err := read(ctx, p.ID)
		if err != nil || got != p {
			t.Fatalf("read = %#v, %v", got, err)
		}
	}
	other := testPost(p.Slug)
	if err := s.Create(ctx, other); !errors.Is(err, blog.ErrSlugExists) {
		t.Fatal("duplicate slug", err)
	}
	if err := s.Create(ctx, p); !errors.Is(err, blog.ErrConflict) && !errors.Is(err, blog.ErrSlugExists) {
		t.Fatal("duplicate ID", err)
	}
	duplicateID := p
	duplicateID.Slug = "another-slug"
	if err := s.Create(ctx, duplicateID); !errors.Is(err, blog.ErrConflict) {
		t.Fatal("duplicate ID with a different slug", err)
	}
	changed := p
	changed.Title = "Updated"
	changed.PublishedAt = time.Now().Add(time.Hour)
	if err := s.Update(ctx, changed); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetByID(ctx, p.ID)
	if err != nil || got.Title != "Updated" || got.Version != 2 || !got.PublishedAt.Equal(p.PublishedAt) {
		t.Fatalf("update = %#v %v", got, err)
	}
	if err := s.Update(ctx, p); !errors.Is(err, blog.ErrConflict) {
		t.Fatal("stale update", err)
	}
	if err := s.Delete(ctx, p.ID, 1); !errors.Is(err, blog.ErrConflict) {
		t.Fatal("stale delete", err)
	}
	if err := New(db).Initialize(ctx); err != nil {
		t.Fatal("reinitialize", err)
	}
	if got, err := New(db).GetByID(ctx, p.ID); err != nil || got.Version != 2 {
		t.Fatal("lost post", err)
	}
	if err := s.Delete(ctx, p.ID, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetByID(ctx, p.ID); !errors.Is(err, blog.ErrNotFound) {
		t.Fatal("deleted post", err)
	}
	if err := s.Delete(ctx, p.ID, 2); !errors.Is(err, blog.ErrConflict) {
		t.Fatal("repeated delete", err)
	}
}

func TestOrderingAndConflictAtomicity(t *testing.T) {
	s, _ := openTest(t)
	ctx := t.Context()
	p, q := testPost("one"), testPost("two")
	q.PublishedAt = p.PublishedAt.Add(time.Hour)
	for _, v := range []blog.Post{p, q} {
		if err := s.Create(ctx, v); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.List(ctx, 0, 1)
	if err != nil || len(got) != 1 || got[0].ID != q.ID {
		t.Fatal(got, err)
	}
	got, err = s.List(ctx, 1, 1)
	if err != nil || len(got) != 1 || got[0].ID != p.ID {
		t.Fatal(got, err)
	}
	p.Slug = q.Slug
	if err := s.Update(ctx, p); !errors.Is(err, blog.ErrSlugExists) {
		t.Fatal(err)
	}
	stored, err := s.GetByID(ctx, p.ID)
	if err != nil || stored.Slug != "one" || stored.Version != 1 {
		t.Fatal(stored, err)
	}
	for _, bounds := range [][2]int{{-1, 1}, {0, 0}, {0, blog.MaxListSize + 1}} {
		if _, err := s.List(ctx, bounds[0], bounds[1]); err == nil {
			t.Fatal("invalid list accepted")
		}
	}
}

func TestConcurrentUpdateAndDelete(t *testing.T) {
	for _, remove := range []bool{false, true} {
		s, _ := openTest(t)
		p := testPost("race")
		if err := s.Create(t.Context(), p); err != nil {
			t.Fatal(err)
		}
		results := make(chan error, 2)
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ {
			wg.Go(func() {
				if remove {
					results <- s.Delete(t.Context(), p.ID, 1)
				} else {
					results <- s.Update(t.Context(), p)
				}
			})
		}
		wg.Wait()
		close(results)
		success, conflicts := 0, 0
		for err := range results {
			if err == nil {
				success++
			} else if errors.Is(err, blog.ErrConflict) {
				conflicts++
			} else {
				t.Fatal(err)
			}
		}
		if success != 1 || conflicts != 1 {
			t.Fatal(success, conflicts)
		}
	}
}

func TestImagesAndFailures(t *testing.T) {
	s, db := openTest(t)
	ctx := t.Context()
	img := blog.Image{ID: uuid.NewString(), MediaType: "image/png", Data: []byte("validated upstream")}
	if err := s.Save(ctx, img); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, img); err == nil {
		t.Fatal("overwrote immutable image")
	}
	got, err := s.Get(ctx, img.ID)
	if err != nil || string(got.Data) != string(img.Data) {
		t.Fatal(got, err)
	}
	got.Data[0] = 'x'
	got, err = s.Get(ctx, img.ID)
	if err != nil || string(got.Data) != string(img.Data) {
		t.Fatal("shared data", err)
	}
	if _, err := s.Get(ctx, uuid.NewString()); !errors.Is(err, blog.ErrNotFound) {
		t.Fatal(err)
	}
	img.MediaType = "image/svg+xml"
	if err := s.Save(ctx, img); err == nil {
		t.Fatal("accepted SVG")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := s.Initialize(canceled); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := s.List(canceled, 0, 1); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := s.Create(canceled, testPost("canceled")); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, img.ID); err == nil {
		t.Fatal("closed storage succeeded")
	}
	if err := New(nil).Initialize(ctx); err == nil {
		t.Fatal("nil pool accepted")
	}
}
