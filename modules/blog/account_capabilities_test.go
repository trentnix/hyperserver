package blog

import (
	"context"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

// This store deliberately exposes neither tokens nor invitation redemption.
type singleUserAccounts struct {
	user.AccountRepository
	user.AdministrationRepository
}

func TestBlogOptionalInvitationStorage(t *testing.T) {
	for _, multiUser := range []bool{false, true} {
		name := "single user"
		if multiUser {
			name = "multiple users"
		}
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			settings := *h.blog.settings
			settings.MultiUser = multiUser
			settings.Storage.Connection = "capabilities.db?_journal=WAL"
			m := &module{settings: &settings, access: &Access{}, accounts: singleUserAccounts{h.app.Accounts, h.blog.administration}}
			err := m.Init(t.Context(), h.blog.app)
			if multiUser {
				if err == nil || !strings.Contains(err.Error(), "invitation support") {
					if err == nil {
						m.Shutdown(context.Background())
					}
					t.Fatalf("missing invitation support: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := m.Shutdown(context.Background()); err != nil {
					t.Error(err)
				}
			})
			if _, err := m.posts.List(t.Context()); err != nil {
				t.Fatal("single-user content storage is unusable", err)
			}
		})
	}
}
