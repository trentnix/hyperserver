package blog

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

// TestWritingAuthorization calls operations directly so route guards cannot mask missing checks.
func TestWritingAuthorization(t *testing.T) {
	actors := []struct {
		name    string
		account *user.User
		writer  bool
		admin   bool
	}{
		{name: "anonymous"},
		{name: "missing ID", account: &user.User{Role: "admin"}},
		{name: "unverified", account: &user.User{ID: "actor", Role: "admin", VerificationRequired: true}},
		{name: "invited", account: &user.User{ID: "actor", Role: "admin", Invited: true}},
		{name: "removed", account: &user.User{ID: "actor"}},
		{name: "unknown role", account: &user.User{ID: "actor", Role: "editor"}},
		{name: "Poster", account: &user.User{ID: "actor", Role: "poster"}, writer: true},
		{name: "Admin", account: &user.User{ID: "actor", Role: "admin"}, writer: true, admin: true},
	}
	for _, actor := range actors {
		t.Run(actor.name, func(t *testing.T) {
			h := newHarness(t)
			ctx := context.WithValue(t.Context(), user.UserContextKey, actor.account)
			check := func(t *testing.T, err error, allowed bool) {
				t.Helper()
				if allowed && err != nil || !allowed && !errors.Is(err, errAccessDenied) {
					t.Fatalf("allowed=%t, got %v", allowed, err)
				}
			}
			t.Run("new post", func(t *testing.T) {
				p, err := h.blog.writePost(ctx, Post{Title: "New post", Slug: "new-post", Markdown: "Body", Draft: true, AuthorID: "forged"})
				check(t, err, actor.writer)
				if actor.writer {
					if p.AuthorID != actor.account.ID {
						t.Fatal("new post accepted a submitted owner")
					}
				} else if _, err := h.blog.posts.GetBySlug(t.Context(), "new-post", true); !errors.Is(err, errNotFound) {
					t.Fatal("denied write created a post", err)
				}
			})
			for _, owner := range []string{"actor", "other"} {
				for _, action := range []string{"edit", "publish", "restore", "unchanged restore"} {
					t.Run(owner+" post "+action, func(t *testing.T) {
						p, err := h.blog.persistPost(t.Context(), Post{Title: owner + " " + action, Markdown: "Original", Draft: true, AuthorID: owner})
						if err != nil {
							t.Fatal(err)
						}
						before, err := h.blog.history.ListRevisions(t.Context(), "post", p.ID)
						if err != nil {
							t.Fatal(err)
						}
						allowed := actor.admin || actor.writer && owner == "actor"
						if action == "restore" || action == "unchanged restore" {
							selected, loadErr := h.blog.history.GetRevision(t.Context(), "post", p.ID, before[0].ID)
							if loadErr != nil {
								t.Fatal(loadErr)
							}
							if action == "restore" {
								selected.Markdown = "Restored"
							}
							err = h.blog.restoreRevisionContent(ctx, "post", p.ID, p.Version, selected)
						} else {
							edited := p
							edited.AuthorID = "forged"
							if owner == "other" {
								edited.AuthorID = "actor"
							}
							if action == "publish" {
								edited.Draft = false
							} else {
								edited.Markdown = "Edited"
							}
							_, err = h.blog.writePost(ctx, edited)
						}
						check(t, err, allowed)
						stored, err := h.blog.posts.GetByID(t.Context(), p.ID, true)
						if err != nil || stored.AuthorID != owner {
							t.Fatal("write changed post ownership", err)
						}
						if !allowed {
							after, err := h.blog.history.ListRevisions(t.Context(), "post", p.ID)
							if err != nil || stored != p || !reflect.DeepEqual(before, after) {
								t.Fatal("denied write changed the post or history", err)
							}
						}
						if allowed && action == "publish" && stored.Draft {
							t.Fatal("authorized publication did not publish")
						}
						if allowed && (action == "edit" && stored.Markdown != "Edited" || action == "restore" && stored.Markdown != "Restored" || action == "unchanged restore" && stored != p) {
							t.Fatal("authorized write did not preserve the requested content")
						}
					})
				}
			}
			t.Run("new page", func(t *testing.T) {
				_, err := h.blog.writePage(ctx, Page{Title: "New page", Slug: "new-page", ChildrenDisplay: "hidden"})
				check(t, err, actor.admin)
				if !actor.admin {
					if _, err := h.blog.pageStore.GetPageBySlug(t.Context(), "new-page", true); !errors.Is(err, errNotFound) {
						t.Fatal("denied write created a page", err)
					}
				}
			})
			for _, action := range []string{"edit", "publish", "restore", "unchanged restore"} {
				t.Run("page "+action, func(t *testing.T) {
					p, err := h.blog.persistPage(t.Context(), Page{Title: "Page " + action, Markdown: "Original", ChildrenDisplay: "hidden"})
					if err != nil {
						t.Fatal(err)
					}
					before, err := h.blog.history.ListRevisions(t.Context(), "page", p.ID)
					if err != nil {
						t.Fatal(err)
					}
					if action == "restore" || action == "unchanged restore" {
						selected, loadErr := h.blog.history.GetRevision(t.Context(), "page", p.ID, before[0].ID)
						if loadErr != nil {
							t.Fatal(loadErr)
						}
						if action == "restore" {
							selected.Markdown = "Restored"
						}
						err = h.blog.restoreRevisionContent(ctx, "page", p.ID, p.Version, selected)
					} else {
						edited := p
						if action == "publish" {
							edited.Published = true
						} else {
							edited.Markdown = "Edited"
						}
						_, err = h.blog.writePage(ctx, edited)
					}
					check(t, err, actor.admin)
					stored, err := h.blog.pageStore.GetPageByID(t.Context(), p.ID, true)
					if err != nil {
						t.Fatal(err)
					}
					if !actor.admin {
						after, err := h.blog.history.ListRevisions(t.Context(), "page", p.ID)
						if err != nil || !reflect.DeepEqual(stored, p) || !reflect.DeepEqual(before, after) {
							t.Fatal("denied write changed the page or history", err)
						}
					}
					if actor.admin && (action == "edit" && stored.Markdown != "Edited" || action == "restore" && stored.Markdown != "Restored" || action == "publish" && !stored.Published || action == "unchanged restore" && !reflect.DeepEqual(stored, p)) {
						t.Fatal("authorized write did not preserve the requested content")
					}
				})
			}
			t.Run("profile", func(t *testing.T) {
				original := h.blog.profile.Current()
				before, err := h.blog.profileStore.ListProfileRevisions(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				edited := original
				edited.Name = "Edited identity"
				check(t, h.blog.writeProfile(ctx, edited), actor.admin)
				stored, err := h.blog.profileStore.GetProfile(t.Context())
				if err != nil || stored != h.blog.profile.Current() {
					t.Fatal("profile storage and publication disagree", err)
				}
				if !actor.admin {
					after, err := h.blog.profileStore.ListProfileRevisions(t.Context())
					if err != nil || stored != original || !reflect.DeepEqual(before, after) {
						t.Fatal("denied write changed the profile or history", err)
					}
				}
				if actor.admin && stored.Name != edited.Name {
					t.Fatal("authorized profile write did not save")
				}
			})
		})
	}
}

type failedWritingRole struct {
	user.AdministrationRepository
	err error
}

func (s failedWritingRole) InitializeRole(context.Context, string, string) error {
	return s.err
}

// TestWritingAuthorizationFailure preserves access-service errors and stops before persistence.
func TestWritingAuthorizationFailure(t *testing.T) {
	failure := errors.New("role assignment failed")
	m := &module{access: &Access{emailKey: user.EmailKey("admin@example.test"), administration: failedWritingRole{err: failure}}}
	ctx := context.WithValue(t.Context(), user.UserContextKey, &user.User{ID: "actor", Email: "admin@example.test"})
	for _, operation := range []struct {
		name string
		call func() error
	}{
		{"post", func() error { _, err := m.writePost(ctx, Post{}); return err }},
		{"page", func() error { _, err := m.writePage(ctx, Page{}); return err }},
		{"profile", func() error { return m.writeProfile(ctx, Profile{}) }},
	} {
		t.Run(operation.name, func(t *testing.T) {
			if err := operation.call(); !errors.Is(err, failure) {
				t.Fatalf("lost authorization failure: %v", err)
			}
		})
	}
}
