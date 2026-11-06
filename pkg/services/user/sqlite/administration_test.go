package sqlite

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

func administrationFixture(t *testing.T) (*AccountRepository, *user.User) {
	t.Helper()
	s := NewAccountRepository(userTestDB(t))
	admin := &user.User{Email: "admin@example.test", Role: "admin", Verified: true, RegistrationAuthType: "email", Password: "existing hash"}
	if err := s.Create(t.Context(), admin); err != nil {
		t.Fatal(err)
	}
	return s, admin
}

func inviteFixture(t *testing.T, s *AccountRepository, admin *user.User, email string) (*user.User, *user.AuthToken) {
	t.Helper()
	u := &user.User{ID: email, Email: email, Role: "poster"}
	token, err := user.NewInvitationToken(u, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.InviteUser(t.Context(), admin.ID, "admin", u, token.Metadata()); err != nil {
		t.Fatal(err)
	}
	return u, token
}

func TestInvitedRegistration(t *testing.T) {
	for _, proof := range []string{"link", "verification", "external"} {
		t.Run(proof, func(t *testing.T) {
			s, admin := administrationFixture(t)
			pending, invitation := inviteFixture(t, s, admin, "writer@example.test")
			u := &user.User{Email: pending.Email, Password: "new hash", RegistrationAuthType: "email", VerificationRequired: true}
			var token *user.TokenMetadata
			var identity *user.ExternalIdentity
			if proof == "link" {
				metadata := invitation.Metadata()
				token = &metadata
			}
			if proof == "external" {
				u.Verified = true
				u.Password = ""
				u.RegistrationAuthType = "google"
				identity = &user.ExternalIdentity{Issuer: "issuer", Subject: "subject"}
			}
			if err := s.RegisterInvited(t.Context(), u, token, identity); err != nil {
				t.Fatal(err)
			}
			if u.ID != pending.ID || u.Role != "poster" || u.Verified != (proof != "verification") || u.Invited != (proof == "verification") || !u.VerificationRequired {
				t.Fatalf("unexpected registration: %+v", u)
			}
			if proof == "verification" {
				verify, err := user.NewAuthVerificationToken(u, time.Hour)
				if err != nil {
					t.Fatal(err)
				}
				if err := s.InsertToken(t.Context(), verify.Metadata()); err != nil {
					t.Fatal(err)
				}
				u, err = s.Verify(t.Context(), user.VerificationAuthorization{AccountID: u.ID, Email: u.Email, TokenHash: verify.TokenHash, ExpiresAt: verify.ExpiresAt})
				if err != nil || u.Invited || !u.Verified {
					t.Fatalf("verification did not accept invitation: %+v %v", u, err)
				}
			}
			if _, err := s.GetToken(t.Context(), invitation.TokenHash, user.InvitationTokenPurpose); !errors.Is(err, user.ErrTokenNotFound) {
				t.Fatalf("invitation remains usable: %v", err)
			}
			if err := s.RegisterInvited(t.Context(), &user.User{Email: u.Email, Password: "replacement", RegistrationAuthType: "email"}, token, nil); !errors.Is(err, user.ErrEmailAlreadyExists) {
				t.Fatalf("credentials could be replaced: %v", err)
			}
		})
	}
}

func TestInvitationReplacementExpiryAndRevocation(t *testing.T) {
	s, admin := administrationFixture(t)
	u, old := inviteFixture(t, s, admin, "writer@example.test")
	newToken, _ := user.NewInvitationToken(u, time.Hour)
	if err := s.InviteUser(t.Context(), admin.ID, "admin", u, newToken.Metadata()); err != nil {
		t.Fatal(err)
	}
	if _, err := user.ValidateInvitation(t.Context(), s, old.Token); !errors.Is(err, user.ErrTokenNotFound) {
		t.Fatalf("old link accepted: %v", err)
	}
	registration := &user.User{Email: u.Email, Password: "hash", RegistrationAuthType: "email"}
	wrong := newToken.Metadata()
	wrong.Email = "wrong@example.test"
	if err := s.RegisterInvited(t.Context(), registration, &wrong, nil); !errors.Is(err, user.ErrToken) {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("UPDATE usertoken SET expires_at=? WHERE token_hash=?", time.Now().Add(-time.Minute), newToken.TokenHash); err != nil {
		t.Fatal(err)
	}
	metadata := newToken.Metadata()
	if err := s.RegisterInvited(t.Context(), registration, &metadata, nil); !errors.Is(err, user.ErrTokenExpired) {
		t.Fatalf("expired token accepted: %v", err)
	}
	stored, _ := s.GetByID(t.Context(), u.ID)
	if stored.Password != "" || !stored.Invited {
		t.Fatal("failed registration changed account")
	}
	u.Role = ""
	if err := s.ChangeUserRole(t.Context(), admin.ID, "admin", u); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterInvited(t.Context(), registration, nil, nil); !errors.Is(err, user.ErrEmailAlreadyExists) {
		t.Fatalf("canceled invitation accepted: %v", err)
	}
}

func TestChangeUserRoleRejectsRemovedAccount(t *testing.T) {
	for _, registered := range []bool{false, true} {
		name := "canceled invitation"
		if registered {
			name = "removed account"
		}
		t.Run(name, func(t *testing.T) {
			s, admin := administrationFixture(t)
			u, invitation := inviteFixture(t, s, admin, "writer@example.test")
			if registered {
				u.Password, u.RegistrationAuthType = "hash", "email"
				metadata := invitation.Metadata()
				if err := s.RegisterInvited(t.Context(), u, &metadata, nil); err != nil {
					t.Fatal(err)
				}
			}
			u.Role = ""
			if err := s.ChangeUserRole(t.Context(), admin.ID, "admin", u); err != nil {
				t.Fatal(err)
			}
			version := u.SessionVersion
			u.Role = "poster"
			if err := s.ChangeUserRole(t.Context(), admin.ID, "admin", u); !errors.Is(err, user.ErrUserNotFound) {
				t.Fatalf("removed account role edit: %v", err)
			}
			stored, err := s.GetByID(t.Context(), u.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Role != "" || stored.Invited || stored.SessionVersion != version {
				t.Fatal("rejected role edit changed account access")
			}
			token, err := user.NewInvitationToken(u, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.InviteUser(t.Context(), admin.ID, "admin", u, token.Metadata()); err != nil {
				t.Fatal(err)
			}
			if !u.Invited || u.Role != "poster" {
				t.Fatal("reinvitation did not restore pending access")
			}
		})
	}
}

func TestInviteExistingAccountPreservesCredentials(t *testing.T) {
	s, admin := administrationFixture(t)
	u := &user.User{Email: "existing@example.test", Password: "keep this hash", RegistrationAuthType: "email", Verified: true}
	if err := s.Create(t.Context(), u); err != nil {
		t.Fatal(err)
	}
	u.Role = "poster"
	token, _ := user.NewInvitationToken(u, time.Hour)
	if err := s.InviteUser(t.Context(), admin.ID, "admin", u, token.Metadata()); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterInvited(t.Context(), &user.User{Email: u.Email, Password: "replacement", RegistrationAuthType: "email"}, nil, nil); !errors.Is(err, user.ErrEmailAlreadyExists) {
		t.Fatal(err)
	}
	before := u.SessionVersion
	if err := s.AcceptInvitation(t.Context(), u, nil); err != nil {
		t.Fatal(err)
	}
	if u.Invited || u.Password != "keep this hash" || u.SessionVersion <= before {
		t.Fatalf("acceptance: %+v", u)
	}
	if err := s.AcceptInvitation(t.Context(), u, nil); !errors.Is(err, user.ErrUserChanged) {
		t.Fatalf("replay accepted: %v", err)
	}
}

func TestReinvitePreservesVerification(t *testing.T) {
	for _, required := range []bool{false, true} {
		s, admin := administrationFixture(t)
		u := &user.User{Email: "existing@example.test", Password: "keep hash", RegistrationAuthType: "email", VerificationRequired: required}
		if err := s.Create(t.Context(), u); err != nil {
			t.Fatal(err)
		}
		u.Role = "poster"
		token, err := user.NewInvitationToken(u, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.InviteUser(t.Context(), admin.ID, "admin", u, token.Metadata()); err != nil {
			t.Fatal(err)
		}
		if u.VerificationRequired != required || u.Verified || !u.Invited {
			t.Fatal("reinvitation changed verification state")
		}
		err = s.AcceptInvitation(t.Context(), u, nil)
		if required {
			if !errors.Is(err, user.ErrUserChanged) || !u.Invited || u.Verified {
				t.Fatal("accepted without required verification", err)
			}
			proof := token.Metadata()
			if err := s.AcceptInvitation(t.Context(), u, &proof); err != nil || !u.Verified {
				t.Fatal("valid link did not prove email ownership", err)
			}
		} else if err != nil || u.Invited || u.Verified {
			t.Fatal("optional verification blocked access or claimed email proof", err)
		}
		if u.Password != "keep hash" {
			t.Fatal("acceptance changed credentials")
		}
	}
}

func TestRoleChangesProtectLastAdminAndRevokeSessions(t *testing.T) {
	s, admin := administrationFixture(t)
	old := *admin
	admin.Role = "poster"
	if err := s.ChangeUserRole(t.Context(), admin.ID, "admin", admin); !errors.Is(err, user.ErrLastAdministrator) {
		t.Fatal(err)
	}
	admin = &old
	poster, token := inviteFixture(t, s, admin, "poster@example.test")
	metadata := token.Metadata()
	registration := &user.User{Email: poster.Email, Password: "hash", RegistrationAuthType: "email"}
	if err := s.RegisterInvited(t.Context(), registration, &metadata, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeUserRole(t.Context(), registration.ID, "admin", admin); !errors.Is(err, user.ErrAccessDenied) {
		t.Fatal(err)
	}
	version := registration.SessionVersion
	registration.Role = "admin"
	if err := s.ChangeUserRole(t.Context(), admin.ID, "admin", registration); err != nil {
		t.Fatal(err)
	}
	if registration.SessionVersion <= version {
		t.Fatal("role change kept old sessions")
	}
	self := *registration
	self.Role = ""
	if err := s.ChangeUserRole(t.Context(), self.ID, "admin", &self); !errors.Is(err, user.ErrRemoveSelf) {
		t.Fatalf("admin removed themselves while another admin exists: %v", err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, current := range []*user.User{admin, registration} {
		wg.Go(func() {
			copy := *current
			copy.Role = "poster"
			results <- s.ChangeUserRole(t.Context(), current.ID, "admin", &copy)
		})
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, user.ErrLastAdministrator) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent demotions succeeded %d times", successes)
	}
}

func TestInvitationRegistrationRollback(t *testing.T) {
	s, admin := administrationFixture(t)
	identity := user.ExternalIdentity{Issuer: "issuer", Subject: "taken"}
	owner := &user.User{Email: "owner@example.test", Verified: true, RegistrationAuthType: "google"}
	if err := s.CreateExternalAccount(t.Context(), owner, identity); err != nil {
		t.Fatal(err)
	}
	pending, token := inviteFixture(t, s, admin, "writer@example.test")
	registration := &user.User{Email: pending.Email, Verified: true, RegistrationAuthType: "google"}
	before := *registration
	metadata := token.Metadata()
	if err := s.RegisterInvited(t.Context(), registration, &metadata, &identity); !errors.Is(err, user.ErrIdentityConflict) {
		t.Fatalf("conflicting identity: %v", err)
	}
	if *registration != before {
		t.Fatal("failed registration changed caller")
	}
	stored, err := s.GetByID(t.Context(), pending.ID)
	if err != nil || stored.RegistrationAuthType != "" || !stored.Invited || stored.Verified {
		t.Fatalf("failed registration changed pending account: %+v %v", stored, err)
	}
	if _, err := user.ValidateInvitation(t.Context(), s, token.Token); err != nil {
		t.Fatal("failed registration consumed token", err)
	}
	// Pending admins cannot replace the last accepted admin.
	pending.Role = "admin"
	if err := s.ChangeUserRole(t.Context(), admin.ID, "admin", pending); err != nil {
		t.Fatal(err)
	}
	admin.Role = "poster"
	if err := s.ChangeUserRole(t.Context(), admin.ID, "admin", admin); !errors.Is(err, user.ErrLastAdministrator) {
		t.Fatal(err)
	}
}
