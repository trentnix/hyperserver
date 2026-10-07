package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/database"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestAuthManagerInitUsesAccountRepository(t *testing.T) {
	// Initialization must preserve the supplied reader without calling it.
	reader := &struct{ user.AccountRepository }{}
	app := &server.ApplicationServer{
		Config:            &config.Config{Auth: config.AuthConfig{Enabled: true}},
		AccountRepository: reader,
		ContentManager:    content.NewContentManager(),
	}
	manager := &AuthManager{}
	if err := manager.Init(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	if manager.accounts != reader {
		t.Fatal("initialization did not preserve the application-supplied account repository")
	}
}

func newLookupTestManager(t *testing.T) (*AuthManager, *user.User, *session.SessionManager, *sqlx.DB) {
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
	manager := &AuthManager{verificationJwtKey: strings.Repeat("t", 32), contentManager: &content.ContentManagerService{
		HomeURL: "/", AuthURL: "/login",
		HandleError: func(w http.ResponseWriter, r *http.Request, message string, err error, status int) {
			http.Error(w, message, status)
		},
	}}
	manager.accounts = user.NewSQLiteAccountRepository(db)
	sessions, err := session.NewSessionManager(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sessions.Close() })
	return manager, u, sessions, db
}

func TestVerifyStopsBeforeRedemptionOnAccountLookupFailure(t *testing.T) {
	for _, failure := range []string{"missing session manager", "invalid session cookie", "account storage unavailable"} {
		t.Run(failure, func(t *testing.T) {
			manager, account, sessions, db := newLookupTestManager(t)
			token, err := user.NewAuthVerificationToken(account, []byte(manager.verificationJwtKey), time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			if err := token.Create(db); err != nil {
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
				if _, err := db.Exec(`ALTER TABLE user RENAME TO unavailable_user`); err != nil {
					t.Fatal(err)
				}
			}
			w := httptest.NewRecorder()
			manager.Verify(w, r)
			if w.Code != http.StatusInternalServerError || w.Body.String() != "Unable to verify your account. Please try again later.\n" || w.Header().Get("HX-Redirect") != "" || len(w.Result().Cookies()) != 0 {
				t.Fatalf("lookup failure response = %d %q, headers=%v", w.Code, w.Body.String(), w.Header())
			}

			if failure == "account storage unavailable" {
				if _, err := db.Exec(`ALTER TABLE unavailable_user RENAME TO user`); err != nil {
					t.Fatal(err)
				}
			}
			stored, err := user.GetUserByID(db, account.ID)
			if err != nil || stored.Verified || !stored.UpdatedAt.Equal(account.UpdatedAt) {
				t.Fatalf("failed lookup changed the account: %+v, %v", stored, err)
			}
			if _, err := user.GetAuthVerificationTokenByHash(db, token.TokenHash); err != nil {
				t.Fatalf("failed lookup consumed the token: %v", err)
			}

			// After repairing the request or storage, the same token must still work.
			w = httptest.NewRecorder()
			manager.Verify(w, session.AddSessionManagerToRequestContext(newRequest(), sessions))
			if w.Code != http.StatusOK || w.Header().Get("HX-Redirect") != "/" {
				t.Fatalf("verification retry failed: %d %s", w.Code, w.Body.String())
			}
			stored, err = user.GetUserByID(db, account.ID)
			if err != nil || !stored.Verified {
				t.Fatalf("retry did not verify the account: %+v, %v", stored, err)
			}
			var count int
			if err := db.Get(&count, `SELECT count(*) FROM usertoken`); err != nil || count != 0 {
				t.Fatalf("retry left tokens: count=%d, error=%v", count, err)
			}
		})
	}
}

type verificationRepository struct {
	user.AccountRepository
	verify func(context.Context, user.VerificationAuthorization) (*user.User, error)
}

func (s verificationRepository) Verify(ctx context.Context, authorization user.VerificationAuthorization) (*user.User, error) {
	return s.verify(ctx, authorization)
}

func TestVerifyUsesAccountRepository(t *testing.T) {
	for _, scenario := range []string{"success", "storage failure"} {
		t.Run(scenario, func(t *testing.T) {
			manager, account, sessions, _ := newLookupTestManager(t)
			token, err := user.NewAuthVerificationToken(account, []byte(manager.verificationJwtKey), time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(http.MethodPost, "/auth/verify", strings.NewReader(url.Values{"token": {token.Token}}.Encode()))
			r = session.AddSessionManagerToRequestContext(r, sessions)
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set("HX-Request", "true")
			calls := 0
			manager.accounts = verificationRepository{verify: func(ctx context.Context, authorization user.VerificationAuthorization) (*user.User, error) {
				calls++
				if ctx != r.Context() || authorization.AccountID != account.ID || authorization.Email != account.Email || authorization.TokenHash != token.TokenHash {
					t.Fatal("verification lost its request context or token identity")
				}
				if scenario == "storage failure" {
					return nil, errors.New("private storage failure")
				}
				account.Verified = true
				account.SessionVersion++
				return account, nil
			}}
			w := httptest.NewRecorder()
			manager.Verify(w, r)
			if calls != 1 {
				t.Fatalf("repository calls=%d, want 1", calls)
			}
			if scenario == "success" {
				if w.Code != http.StatusOK || w.Header().Get("HX-Redirect") != "/" {
					t.Fatalf("verification did not redirect after success: %d %s", w.Code, w.Body.String())
				}
			} else if w.Code != http.StatusBadRequest || w.Header().Get("HX-Redirect") != "" || !strings.Contains(w.Body.String(), "Verification failed") || strings.Contains(w.Body.String(), "private storage failure") {
				t.Fatalf("failed verification rendered an incorrect response: %d %s", w.Code, w.Body.String())
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

type tokenLookupRepository struct {
	user.AccountRepository
	token   func(context.Context, string, string) (user.TokenMetadata, error)
	account func(context.Context, string) (*user.User, error)
}

func (s tokenLookupRepository) GetToken(ctx context.Context, hash, purpose string) (user.TokenMetadata, error) {
	return s.token(ctx, hash, purpose)
}

func (s tokenLookupRepository) GetByID(ctx context.Context, id string) (*user.User, error) {
	return s.account(ctx, id)
}

func TestResetLookupUsesAccountRepository(t *testing.T) {
	previous := authServices
	t.Cleanup(func() { authServices = previous })
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		for _, scenario := range []string{"success", "token failure", "account failure", "canceled"} {
			t.Run(method+"/"+scenario, func(t *testing.T) {
				account := &user.User{ID: "account", Email: "person@example.invalid"}
				key := "test-signing-key"
				token, err := user.NewAuthResetToken(account, []byte(key), time.Hour)
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if scenario == "canceled" {
					cancel()
				}
				tokenCalls, accountCalls := 0, 0
				repository := tokenLookupRepository{
					token: func(got context.Context, hash, purpose string) (user.TokenMetadata, error) {
						tokenCalls++
						if got != ctx || hash != token.TokenHash || purpose != token.Type {
							t.Fatal("token lookup lost the request context or token identity")
						}
						if scenario == "token failure" {
							return user.TokenMetadata{}, errors.New("private storage failure")
						}
						if err := got.Err(); err != nil {
							return user.TokenMetadata{}, err
						}
						return token.Metadata(), nil
					},
					account: func(got context.Context, id string) (*user.User, error) {
						accountCalls++
						if got != ctx || id != account.ID {
							t.Fatal("account lookup lost the request context or account ID")
						}
						if scenario == "account failure" {
							return nil, errors.New("private storage failure")
						}
						return account, nil
					},
				}
				manager := &AuthManager{accounts: repository, verificationJwtKey: key, contentManager: &content.ContentManagerService{
					HandleError: func(w http.ResponseWriter, r *http.Request, message string, err error, status int) {
						http.Error(w, message, status)
					},
				}}
				provider := &lookupTestAuthService{}
				authServices = []AuthService{provider}
				r := httptest.NewRequest(method, "/auth/reset/lookup-test?token="+url.QueryEscape(token.Token), nil).WithContext(ctx)
				r.SetPathValue("authType", "lookup-test")
				w := httptest.NewRecorder()
				if method == http.MethodGet {
					manager.GetReset(w, r)
				} else {
					manager.Reset(w, r)
				}
				wantAccountCalls := 0
				if scenario == "success" || scenario == "account failure" {
					wantAccountCalls = 1
				}
				if tokenCalls != 1 || accountCalls != wantAccountCalls {
					t.Fatalf("token calls=%d, account calls=%d", tokenCalls, accountCalls)
				}
				if scenario == "success" {
					if w.Code != http.StatusNoContent || provider.calls != 1 {
						t.Fatal("valid lookup did not reach authentication provider")
					}
				} else if w.Code != http.StatusInternalServerError || provider.calls != 0 || strings.Contains(w.Body.String(), "private storage failure") {
					t.Fatalf("lookup failure continued or leaked details: %d %s", w.Code, w.Body.String())
				}
			})
		}
	}
}

func TestResetStopsOnTokenLookupFailure(t *testing.T) {
	previous := authServices
	t.Cleanup(func() { authServices = previous })
	for _, table := range []string{"usertoken", "user"} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			t.Run(table+"/"+method, func(t *testing.T) {
				manager, account, _, db := newLookupTestManager(t)
				provider := &lookupTestAuthService{}
				authServices = []AuthService{provider}
				token, err := user.NewAuthResetToken(account, []byte(manager.verificationJwtKey), time.Hour)
				if err != nil {
					t.Fatal(err)
				}
				if err := token.Create(db); err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec("ALTER TABLE " + table + " RENAME TO unavailable"); err != nil {
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

				if _, err := db.Exec("ALTER TABLE unavailable RENAME TO " + table); err != nil {
					t.Fatal(err)
				}
				if _, err := user.ValidateResetToken(context.Background(), manager.accounts, token.Token, []byte(manager.verificationJwtKey)); err != nil {
					t.Fatalf("failed lookup invalidated the token: %v", err)
				}
				stored, err := user.GetUserByID(db, account.ID)
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
