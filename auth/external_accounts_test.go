package auth

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/user"
	accountsqlite "github.com/trentnix/hyperserver/pkg/services/user/sqlite"
)

func TestExternalAccountResolution(t *testing.T) {
	for _, mode := range []string{"register", "missing verified email", "disabled", "closed", "denied", "policy failure", "existing email", "login", "unlinked login", "link", "anonymous link", "wrong account", "wrong version", "revoked version", "unverified link", "unknown intent", "missing identity"} {
		t.Run(mode, func(t *testing.T) {
			db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "accounts.db")+"?_fk=true")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Close() })
			repo := accountsqlite.NewAccountRepository(db)
			if err := repo.Initialize(t.Context()); err != nil {
				t.Fatal(err)
			}
			cm := contentservice.NewContentManager()
			cm.HandleError = func(w http.ResponseWriter, r *http.Request, message string, _ error, status int) {
				http.Error(w, message, status)
			}
			resolver := ExternalAccounts{Identities: repo, Content: cm, Provider: "example", RegistrationEnabled: true}
			proof := ExternalAccountRequest{Mode: "register", Identity: user.ExternalIdentity{Issuer: "https://identity.example", Subject: "one"}, VerifiedEmail: "person@example.com"}
			r := httptest.NewRequest("GET", "/auth/callback/example", nil)
			wantStatus, wantAccount, wantCreated := 200, true, true
			switch mode {
			case "missing verified email":
				proof.VerifiedEmail, wantStatus = "", 403
			case "disabled":
				resolver.RegistrationEnabled, wantStatus = false, 403
			case "closed":
				resolver.Policy.BeforeRegister = func(w http.ResponseWriter, _ *http.Request) bool { w.WriteHeader(410); return false }
				wantStatus = 410
			case "denied", "policy failure":
				resolver.Policy.AllowAccount = func(context.Context, string) (bool, error) {
					if mode == "policy failure" {
						return false, errors.New("unavailable")
					}
					return false, nil
				}
				wantStatus = 403
				if mode == "policy failure" {
					wantStatus = 503
				}
			case "existing email":
				if err := repo.Create(t.Context(), &user.User{Email: proof.VerifiedEmail, Password: "preserved"}); err != nil {
					t.Fatal(err)
				}
				wantStatus = 409
			case "unknown intent":
				proof.Mode, wantStatus = "unexpected", 400
			case "missing identity":
				proof.Identity.Subject, wantStatus = "", 400
			case "login", "unlinked login":
				proof.Mode, proof.VerifiedEmail, wantCreated = "login", "", false
				if mode == "login" {
					if err := repo.CreateExternalAccount(t.Context(), &user.User{Email: "person@example.com", Verified: true}, proof.Identity); err != nil {
						t.Fatal(err)
					}
				} else {
					wantStatus = 403
				}
			default:
				if mode != "register" {
					account := &user.User{Email: "person@example.com", Password: "preserved", Verified: mode != "unverified link", VerificationRequired: true}
					if err := repo.Create(t.Context(), account); err != nil {
						t.Fatal(err)
					}
					proof.Mode, proof.VerifiedEmail, wantCreated = "link", "", false
					proof.AccountID, proof.SessionVersion = account.ID, account.SessionVersion
					r = user.AddUserToRequestContext(r, account)
					switch mode {
					case "anonymous link":
						r = r.WithContext(user.ClearUserFromContext(r.Context()))
					case "wrong account":
						proof.AccountID = "other"
					case "wrong version":
						proof.SessionVersion++
					case "revoked version":
						if _, err := db.Exec(`UPDATE user SET session_version = session_version + 1 WHERE id = ?`, account.ID); err != nil {
							t.Fatal(err)
						}
					}
					if mode != "link" {
						wantStatus = 403
					}
					if mode == "revoked version" {
						wantStatus = 409
					}
				}
			}
			if wantStatus != 200 {
				wantAccount, wantCreated = false, false
			}
			w := httptest.NewRecorder()
			account, created := resolver.Resolve(w, r, proof)
			if w.Code != wantStatus || (account != nil) != wantAccount || created != wantCreated {
				t.Fatalf("status=%d account=%v created=%t: %s", w.Code, account != nil, created, w.Body)
			}
			if len(w.Result().Cookies()) != 0 {
				t.Fatal("account operation created a session")
			}
			if !wantAccount {
				if _, err := repo.GetExternalAccount(t.Context(), proof.Identity); !errors.Is(err, user.ErrUserNotFound) {
					t.Fatal("failed operation linked an identity", err)
				}
			}
			if mode == "link" && (account.Password != "preserved" || account.SessionVersion != 2) {
				t.Fatal("link did not preserve credentials and revoke old sessions")
			}
		})
	}
}
