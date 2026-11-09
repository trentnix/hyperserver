package blog

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestAssignUnownedContent(t *testing.T) {
	settings := Settings{}
	settings.Storage.Provider, settings.Storage.Connection = "sqlite", ":memory:"
	store, closeStore, err := openRepository(t.Context(), settings, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeStore() })
	for _, owner := range []string{"", "other-user"} {
		if _, err := savePostFixture(store, t.Context(), Post{Title: "Post", Slug: "post-" + owner, AuthorID: owner}); err != nil {
			t.Fatal(err)
		}
		if err := store.RememberMedia(t.Context(), "image-"+owner, "image.png", owner); err != nil {
			t.Fatal(err)
		}
	}
	checkOwners := func(adopted string) {
		t.Helper()
		for _, item := range []struct{ query, id, want string }{
			{"SELECT author_id FROM blog_current_posts WHERE slug=?", "post-", adopted},
			{"SELECT author_id FROM blog_current_posts WHERE slug=?", "post-other-user", "other-user"},
			{"SELECT uploaded_by FROM blog_media WHERE id=?", "image-", adopted},
			{"SELECT uploaded_by FROM blog_media WHERE id=?", "image-other-user", "other-user"},
		} {
			var got string
			if err := store.db.QueryRowContext(t.Context(), item.query, item.id).Scan(&got); err != nil || got != item.want {
				t.Fatalf("owner of %s = %q, want %q: %v", item.id, got, item.want, err)
			}
		}
	}

	// Fail the second update after posts have changed. Neither update may commit.
	if _, err := store.db.ExecContext(t.Context(), `CREATE TRIGGER reject_ownership BEFORE UPDATE OF uploaded_by ON blog_media
		BEGIN SELECT RAISE(ABORT, 'ownership unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if err := store.assignUnownedContent(t.Context(), "admin"); err == nil || !strings.Contains(err.Error(), "ownership unavailable") {
		t.Fatalf("ownership failure = %v", err)
	}
	checkOwners("")
	if _, err := store.db.ExecContext(t.Context(), "DROP TRIGGER reject_ownership"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := store.assignUnownedContent(ctx, "admin"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled ownership update = %v", err)
	}
	checkOwners("")
	if err := store.assignUnownedContent(t.Context(), "admin"); err != nil {
		t.Fatal(err)
	}
	checkOwners("admin")
	if err := store.assignUnownedContent(t.Context(), "replacement"); err != nil {
		t.Fatal(err)
	}
	checkOwners("admin")
}
