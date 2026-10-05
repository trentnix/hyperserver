package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/database"
	"github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func newLookupTestManager(t *testing.T) (*AuthManager, *user.User, *session.SessionManager) {
	t.Helper()
	db, err := database.Setup("sqlite3", filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := user.PrepareDatabase(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	u := &user.User{Email: "lookup@example.invalid", Password: "unchanged", VerificationRequired: true, RegistrationAuthType: "lookup-test"}
	if err := u.Create(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.HTTP.Session.JwtKey = strings.Repeat("s", 32)
	cfg.HTTP.Session.TokenAge, cfg.HTTP.Session.CookieAge = time.Hour, time.Hour
	cfg.HTTP.Session.Stores = map[string]map[string]string{"cookieStore": {"enabled": "true"}}
	cfg.HTTP.Session.Types = map[string]string{"default": "cookieStore"}
	manager := &AuthManager{db: db, verificationJwtKey: strings.Repeat("t", 32), contentManager: &content.ContentManagerService{
		HomeURL: "/", AuthURL: "/login",
		HandleError: func(w http.ResponseWriter, r *http.Request, message string, err error, status int) {
			http.Error(w, message, status)
		},
	}}
	sessions, err := session.NewSessionManager(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sessions.Close() })
	return manager, u, sessions
}

func TestVerifyStopsBeforeRedemptionOnAccountLookupFailure(t *testing.T) {
	for _, failure := range []string{"missing session manager", "invalid session cookie", "account storage unavailable"} {
		t.Run(failure, func(t *testing.T) {
			manager, account, sessions := newLookupTestManager(t)
			token, err := user.NewAuthVerificationToken(account, []byte(manager.verificationJwtKey), time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			if err := token.Create(manager.db); err != nil {
				t.Fatal(err)
			}
			newRequest := func() *http.Request {
				r := httptest.NewRequest(http.MethodPost, "/auth/verify", strings.NewReader(url.Values{"token": {token.Token}}.Encode()))
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				r.Header.Set("HX-Request", "true")
				return r
			}
			r := newRequest()
			if failure != "missing session manager" {
				r = session.AddSessionManagerToRequestContext(r, sessions)
			}
			if failure == "invalid session cookie" {
				r.AddCookie(&http.Cookie{Name: "auth-user-session", Value: "invalid.jwt.token"})
			}
			if failure == "account storage unavailable" {
				setup := session.AddSessionManagerToRequestContext(httptest.NewRequest(http.MethodGet, "/", nil), sessions)
				w := httptest.NewRecorder()
				// Seed a legacy cookie directly to exercise account lookup failure.
				s, err := session.New(setup, "auth-user-session")
				if err != nil {
					t.Fatal(err)
				}
				s.Data["auth-user-session"] = account.ID
				if err := s.Save(w, setup); err != nil {
					t.Fatal(err)
				}
				for _, cookie := range w.Result().Cookies() {
					r.AddCookie(cookie)
				}
				if _, err := manager.db.Exec(`ALTER TABLE user RENAME TO unavailable_user`); err != nil {
					t.Fatal(err)
				}
			}
			w := httptest.NewRecorder()
			manager.Verify(w, r)
			if w.Code != http.StatusInternalServerError || w.Body.String() != "Unable to verify your account. Please try again later.\n" || w.Header().Get("HX-Redirect") != "" || len(w.Result().Cookies()) != 0 {
				t.Fatalf("lookup failure response = %d %q, headers=%v", w.Code, w.Body.String(), w.Header())
			}

			if failure == "account storage unavailable" {
				if _, err := manager.db.Exec(`ALTER TABLE unavailable_user RENAME TO user`); err != nil {
					t.Fatal(err)
				}
			}
			stored, err := user.GetUserByID(manager.db, account.ID)
			if err != nil || stored.Verified || !stored.UpdatedAt.Equal(account.UpdatedAt) {
				t.Fatalf("failed lookup changed the account: %+v, %v", stored, err)
			}
			if _, err := user.GetAuthVerificationTokenByHash(manager.db, token.TokenHash); err != nil {
				t.Fatalf("failed lookup consumed the token: %v", err)
			}

			// After repairing the request or storage, the same token must still work.
			w = httptest.NewRecorder()
			manager.Verify(w, session.AddSessionManagerToRequestContext(newRequest(), sessions))
			if w.Code != http.StatusOK || w.Header().Get("HX-Redirect") != "/" {
				t.Fatalf("verification retry failed: %d %s", w.Code, w.Body.String())
			}
			stored, err = user.GetUserByID(manager.db, account.ID)
			if err != nil || !stored.Verified {
				t.Fatalf("retry did not verify the account: %+v, %v", stored, err)
			}
			var count int
			if err := manager.db.Get(&count, `SELECT count(*) FROM usertoken`); err != nil || count != 0 {
				t.Fatalf("retry left tokens: count=%d, error=%v", count, err)
			}
		})
	}
}

type lookupTestAuthService struct {
	AuthService
	calls int
}

func (*lookupTestAuthService) AuthType() string { return "lookup-test" }
func (*lookupTestAuthService) IsLoaded() bool   { return true }
func (s *lookupTestAuthService) GetReset(w http.ResponseWriter, r *http.Request, token string) {
	s.calls++
	w.WriteHeader(http.StatusNoContent)
}
func (s *lookupTestAuthService) Reset(w http.ResponseWriter, r *http.Request, u *user.User, token string, requireNew bool) bool {
	s.calls++
	w.WriteHeader(http.StatusNoContent)
	return false
}

func TestResetStopsOnTokenLookupFailure(t *testing.T) {
	previous := authServices
	t.Cleanup(func() { authServices = previous })
	for _, table := range []string{"usertoken", "user"} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			t.Run(table+"/"+method, func(t *testing.T) {
				manager, account, _ := newLookupTestManager(t)
				provider := &lookupTestAuthService{}
				authServices = []AuthService{provider}
				token, err := user.NewAuthResetToken(account, []byte(manager.verificationJwtKey), time.Hour)
				if err != nil {
					t.Fatal(err)
				}
				if err := token.Create(manager.db); err != nil {
					t.Fatal(err)
				}
				if _, err := manager.db.Exec("ALTER TABLE " + table + " RENAME TO unavailable"); err != nil {
					t.Fatal(err)
				}
				handler := manager.GetReset
				if method == http.MethodPost {
					handler = manager.Reset
				}
				request := func() *http.Request {
					r := httptest.NewRequest(method, "/auth/reset/lookup-test?token="+url.QueryEscape(token.Token), nil)
					r.SetPathValue("authType", "lookup-test")
					return r
				}
				w := httptest.NewRecorder()
				handler(w, request())
				if w.Code != http.StatusInternalServerError || provider.calls != 0 || w.Header().Get("HX-Redirect") != "" {
					t.Fatalf("failed lookup continued: status=%d, provider calls=%d", w.Code, provider.calls)
				}
				if strings.Contains(w.Body.String(), token.Token) || strings.Contains(w.Body.String(), "no such table") {
					t.Fatal("lookup failure exposed token or storage details")
				}

				if _, err := manager.db.Exec("ALTER TABLE unavailable RENAME TO " + table); err != nil {
					t.Fatal(err)
				}
				if _, err := user.ValidateResetToken(manager.db, token.Token, []byte(manager.verificationJwtKey)); err != nil {
					t.Fatalf("failed lookup invalidated the token: %v", err)
				}
				stored, err := user.GetUserByID(manager.db, account.ID)
				if err != nil || stored.Password != account.Password || !stored.UpdatedAt.Equal(account.UpdatedAt) {
					t.Fatalf("failed lookup changed the account: %+v, %v", stored, err)
				}
				w = httptest.NewRecorder()
				handler(w, request())
				if w.Code != http.StatusNoContent || provider.calls != 1 {
					t.Fatalf("repaired lookup did not reach provider: status=%d, calls=%d", w.Code, provider.calls)
				}
			})
		}
	}
}
