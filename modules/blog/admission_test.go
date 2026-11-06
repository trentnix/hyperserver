package blog

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

// admissionInterleave assigns the initial role after the admission check reads
// the account, reproducing a concurrent request rendering site navigation.
type admissionInterleave struct {
	user.AccountRepository
	initialize func() error
}

func (r admissionInterleave) GetByEmail(ctx context.Context, email string) (*user.User, error) {
	account, err := r.AccountRepository.GetByEmail(ctx, email)
	if err == nil {
		err = r.initialize()
	}
	return account, err
}

func TestAccountAdmissionDuringInitialRoleAssignment(t *testing.T) {
	for _, multiUser := range []bool{false, true} {
		for _, hx := range []bool{false, true} {
			name := "single-user"
			if multiUser {
				name = "multi-user"
			}
			if hx {
				name += "/htmx"
			} else {
				name += "/full-page"
			}
			t.Run(name, func(t *testing.T) {
				h := newHarness(t)
				h.blog.access.multiUser = multiUser
				u := &user.User{Email: h.blog.access.setup.email, Verified: true, RegistrationAuthType: "google"}
				if err := h.app.Accounts.(user.ExternalIdentityRepository).CreateExternalAccount(t.Context(), u, user.ExternalIdentity{Issuer: "https://accounts.google.com", Subject: "owner"}); err != nil {
					t.Fatal(err)
				}
				signInAccount(t, h, u)
				interleaved := false
				h.blog.access.accounts = admissionInterleave{
					AccountRepository: h.app.Accounts,
					initialize: func() error {
						interleaved = true
						return h.blog.access.administration.InitializeRole(t.Context(), u.Email, string(roleAdmin))
					},
				}
				requireStatus(t, h.request("GET", "/auth/account", nil, hx), http.StatusOK)
				if !interleaved {
					t.Fatal("admission did not read the account before role assignment")
				}
			})
		}
	}
}

type admissionSnapshot struct {
	user.AdministrationRepository
	users []user.User
	err   error
}

func (s admissionSnapshot) ListUsers(context.Context) ([]user.User, error) {
	return s.users, s.err
}

func TestInitialAccountAdmissionSnapshot(t *testing.T) {
	storageErr := errors.New("storage unavailable")
	for _, tc := range []struct {
		name    string
		account *user.User
		err     error
		allowed bool
	}{
		{name: "empty site", allowed: true},
		{name: "assigned administrator", account: &user.User{Role: "admin", RegistrationAuthType: "google"}, allowed: true},
		{name: "disabled invitation", account: &user.User{Role: "admin", Invited: true}},
		{name: "unknown role", account: &user.User{Role: "unknown", RegistrationAuthType: "google"}},
		{name: "different administrator", account: &user.User{Email: "other@example.test", Role: "admin", RegistrationAuthType: "google"}},
		{name: "storage failure", err: storageErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.blog.access.multiUser = false
			snapshot := admissionSnapshot{err: tc.err}
			if tc.account != nil {
				account := *tc.account
				if account.Email == "" {
					account.Email = h.blog.access.setup.email
				}
				snapshot.users = []user.User{account}
			}
			h.blog.access.administration = snapshot
			allowed, err := h.blog.access.AllowAccount(t.Context(), h.blog.access.setup.email)
			if allowed != tc.allowed || !errors.Is(err, tc.err) {
				t.Fatalf("admission = %t, %v; want %t, %v", allowed, err, tc.allowed, tc.err)
			}
		})
	}
}
