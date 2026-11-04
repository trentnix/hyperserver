package google

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"
	"github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/auth/identity"
	authmiddleware "github.com/trentnix/hyperserver/auth/middleware"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/session"
	sessionsqlite "github.com/trentnix/hyperserver/pkg/services/session/sqlite"
	"github.com/trentnix/hyperserver/pkg/services/user"
	accountsqlite "github.com/trentnix/hyperserver/pkg/services/user/sqlite"
)

type harness struct {
	t            *testing.T
	service      *Service
	accounts     *accountsqlite.AccountRepository
	sessions     *session.SessionManager
	handler      http.Handler
	cookies      []*http.Cookie
	claims       jwt.MapClaims
	challenge    string
	exchanges    atomic.Int32
	tokenFailure bool
	badSignature bool
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "accounts.db")+"?_fk=true&_busy_timeout=5000")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	accounts := accountsqlite.NewAccountRepository(db)
	if err := accounts.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	sessions, err := session.NewSessionManager(t.Context(), map[string]string{"default": "sqlite"}, map[string]session.StoreFactory{
		"sqlite": func(ctx context.Context) (session.SessionStore, error) {
			return sessionsqlite.NewSQLiteStore(ctx, sessionsqlite.Options{Connection: filepath.Join(t.TempDir(), "sessions.db") + "?_busy_timeout=5000", Table: "sessions", SigningKey: strings.Repeat("s", 64), TokenLifetime: time.Hour, CookieLifetime: time.Hour})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sessions.Close() })
	cfg := config.Config{HTTP: config.HTTPConfig{PublicOrigin: "http://localhost"}, Auth: config.AuthConfig{
		Enabled: true, RegistrationEnabled: true, RegisterRequiresVerification: true,
		JwtKey: strings.Repeat("a", 64), ResetTokenExpiration: time.Hour, VerificationTokenExpiration: time.Hour,
		Services: map[string]map[string]string{"google": {"enabled": "true", "clientID": "test-client", "clientSecret": "test-secret"}},
	}}
	app, err := server.NewApplicationServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.Shutdown() })
	app.ContentManager.HomeURL = "/"
	app.ContentManager.AuthURL = "/login"
	layouts := fstest.MapFS{"page.html": {Data: []byte(`{{renderPartial .PartialName .}}`)}}
	app.ContentManager.AddPageLayout(layouts, "page.html")
	app.ContentManager.AddHtmxLayout(layouts, "page.html")
	provider := New(accounts, sessions)
	if err := handlers.Setup(t.Context(), app, nil, []handlers.Descriptor{auth.Module(auth.Dependencies{Accounts: accounts, Sessions: sessions}, []auth.Descriptor{{Name: AuthType, New: func(_ auth.Dependencies) auth.AuthService { return provider }}})}, nil); err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, accounts: accounts, sessions: sessions, service: provider, handler: authmiddleware.LoadSessionManagement(accounts, sessions)(app.Web)}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/keys" {
			json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "test-key", Algorithm: "RS256", Use: "sig"}}})
			return
		}
		h.exchanges.Add(1)
		r.ParseForm()
		challenge := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if base64.RawURLEncoding.EncodeToString(challenge[:]) != h.challenge || r.Form.Get("client_secret") != "test-secret" || r.Form.Get("redirect_uri") != "http://localhost/auth/callback/google" {
			http.Error(w, "bad exchange", 400)
			return
		}
		if h.tokenFailure {
			http.Error(w, "upstream failure", 503)
			return
		}
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, h.claims)
		token.Header["kid"] = "test-key"
		raw, err := token.SignedString(key)
		if err != nil {
			t.Error(err)
			return
		}
		if h.badSignature {
			parts := strings.Split(raw, ".")
			parts[2] = base64.RawURLEncoding.EncodeToString(make([]byte, 256))
			raw = strings.Join(parts, ".")
		}
		json.NewEncoder(w).Encode(map[string]any{"access_token": "unused", "token_type": "Bearer", "id_token": raw})
	}))
	t.Cleanup(remote.Close)
	provider.oauth.Endpoint.TokenURL = remote.URL + "/token"
	provider.verifier = oidc.NewVerifier(issuer, oidc.NewRemoteKeySet(context.Background(), remote.URL+"/keys"), &oidc.Config{ClientID: "test-client"})
	return h
}

func (h *harness) request(method, target string) *httptest.ResponseRecorder {
	h.t.Helper()
	r := httptest.NewRequest(method, "http://localhost"+target, strings.NewReader("provider=google"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range h.cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, r)
	for _, cookie := range w.Result().Cookies() {
		found := false
		for i, old := range h.cookies {
			if old.Name == cookie.Name {
				h.cookies[i] = cookie
				found = true
			}
		}
		if !found {
			h.cookies = append(h.cookies, cookie)
		}
	}
	return w
}

func (h *harness) start(path string) string {
	h.t.Helper()
	w := h.request("POST", path)
	if w.Code != 303 {
		h.t.Fatalf("start: %d %s", w.Code, w.Body)
	}
	location, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		h.t.Fatal(err)
	}
	query := location.Query()
	if location.Host != "accounts.google.com" || query.Get("code_challenge_method") != "S256" || query.Get("scope") != "openid email" {
		h.t.Fatalf("invalid authorization request: %s", location)
	}
	h.challenge = query.Get("code_challenge")
	h.claims = jwt.MapClaims{"iss": issuer, "aud": "test-client", "sub": "google-subject", "email": "admin@gmail.com", "email_verified": true, "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": query.Get("nonce")}
	return callbackPath + "?" + url.Values{"state": {query.Get("state")}, "code": {"test-code"}}.Encode()
}

func (h *harness) linkedAccount() *user.User {
	h.t.Helper()
	account := &user.User{Email: "admin@gmail.com", Verified: true, RegistrationAuthType: AuthType}
	if err := h.accounts.CreateExternalAccount(h.t.Context(), account, user.ExternalIdentity{Issuer: issuer, Subject: "google-subject"}); err != nil {
		h.t.Fatal(err)
	}
	return account
}

func (h *harness) signIn(account *user.User) {
	h.t.Helper()
	r := session.AddSessionManagerToRequestContext(httptest.NewRequest("GET", "http://localhost/", nil), h.sessions)
	w := httptest.NewRecorder()
	if err := identity.SetAuthenticatedUser(w, r, account); err != nil {
		h.t.Fatal(err)
	}
	h.cookies = append(h.cookies, w.Result().Cookies()...)
}

func TestGoogleLoginAndReplay(t *testing.T) {
	h := newHarness(t)
	account := h.linkedAccount()
	callback := h.start("/login")
	oldCookies := append([]*http.Cookie(nil), h.cookies...)
	w := h.request("GET", callback)
	if w.Code != 303 || w.Header().Get("Location") != "/" {
		t.Fatalf("login: %d %s", w.Code, w.Body)
	}
	r := session.AddSessionManagerToRequestContext(httptest.NewRequest("GET", "http://localhost/", nil), h.sessions)
	for _, cookie := range h.cookies {
		r.AddCookie(cookie)
	}
	u, err := identity.GetAuthenticatedUser(r, h.accounts)
	if err != nil || u == nil || u.ID != account.ID {
		t.Fatalf("session: %+v %v", u, err)
	}
	h.cookies = oldCookies
	if w := h.request("GET", callback); w.Code != 400 {
		t.Fatalf("replay accepted: %d", w.Code)
	}
	if h.exchanges.Load() != 1 {
		t.Fatal("replayed flow reached token endpoint")
	}
}

func TestGoogleInvalidFlowCookieCanStartAgain(t *testing.T) {
	h := newHarness(t)
	h.linkedAccount()
	h.cookies = []*http.Cookie{{Name: flowSession, Value: "broken-cookie"}}
	callback := h.start("/login")
	if w := h.request("GET", callback); w.Code != 303 {
		t.Fatalf("invalid cookie prevented retry: %d %s", w.Code, w.Body)
	}
}

func TestGoogleRejectsInvalidProofs(t *testing.T) {
	for _, name := range []string{"state", "missing cookie", "expired flow", "issuer", "audience", "expiry", "signature", "nonce", "subject", "email", "unverified", "denied", "policy failure", "token failure", "canceled", "missing code", "duplicate state"} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.linkedAccount()
			callback := h.start("/login")
			switch name {
			case "state":
				callback = callbackPath + "?state=wrong&code=test-code"
			case "duplicate state":
				callback += "&state=other"
			case "missing cookie":
				h.cookies = nil
			case "expired flow":
				r := session.AddSessionManagerToRequestContext(httptest.NewRequest("GET", "http://localhost/", nil), h.sessions)
				for _, c := range h.cookies {
					r.AddCookie(c)
				}
				s, err := session.Get(r, flowSession)
				if err != nil {
					t.Fatal(err)
				}
				var f flow
				if err := s.DecodeValue("flow", &f); err != nil {
					t.Fatal(err)
				}
				f.Expires = time.Now().Add(-time.Minute)
				s.Data["flow"] = f
				if err := s.Save(httptest.NewRecorder(), r); err != nil {
					t.Fatal(err)
				}
			case "issuer":
				h.claims["iss"] = "https://attacker.invalid"
			case "signature":
				h.badSignature = true
			case "audience":
				h.claims["aud"] = "another-client"
			case "expiry":
				h.claims["exp"] = time.Now().Add(-time.Hour).Unix()
			case "nonce":
				h.claims["nonce"] = "wrong"
			case "subject":
				h.claims["sub"] = ""
			case "email":
				h.claims["email"] = ""
			case "unverified":
				h.claims["email_verified"] = false
			case "denied":
				h.service.Policy.AllowAccount = func(context.Context, string) (bool, error) { return false, nil }
			case "policy failure":
				h.service.Policy.AllowAccount = func(context.Context, string) (bool, error) { return false, errors.New("offline") }
			case "token failure":
				h.tokenFailure = true
			case "canceled":
				callback += "&error=access_denied"
			case "missing code":
				callback = strings.ReplaceAll(callback, "code=test-code&", "")
			}
			w := h.request("GET", callback)
			if w.Code < 400 {
				t.Fatalf("accepted invalid proof: %d %s", w.Code, w.Body)
			}
			if name == "policy failure" && w.Code != 503 {
				t.Fatalf("policy outage returned %d", w.Code)
			}
			for _, cookie := range w.Result().Cookies() {
				if cookie.Name == "auth-user-session" && cookie.MaxAge >= 0 {
					t.Fatal("issued login cookie")
				}
			}
		})
	}
}

func TestGoogleRegistrationAndExplicitLink(t *testing.T) {
	for _, mode := range []string{"register", "registration disabled", "registration closed during redirect", "third party email", "existing email", "link", "changed session"} {
		t.Run(mode, func(t *testing.T) {
			h := newHarness(t)
			var account *user.User
			if mode == "existing email" || mode == "link" || mode == "changed session" {
				account = &user.User{Email: "admin@gmail.com", RegistrationAuthType: "email", Password: "preserved"}
				if err := h.accounts.Create(t.Context(), account); err != nil {
					t.Fatal(err)
				}
			}
			path := "/register"
			if mode == "link" || mode == "changed session" {
				h.signIn(account)
				path = "/auth/link/google"
			}
			callback := h.start(path)
			if mode == "registration disabled" {
				h.service.registration = false
			}
			if mode == "registration closed during redirect" {
				h.service.Policy.BeforeRegister = func(w http.ResponseWriter, _ *http.Request) bool {
					w.WriteHeader(http.StatusGone)
					return false
				}
			}
			if mode == "third party email" {
				h.claims["email"] = "person@example.com"
			}
			if mode == "changed session" {
				h.cookies = h.cookies[len(h.cookies)-1:]
			}
			w := h.request("GET", callback)
			if mode == "register" {
				if w.Code != 200 || !strings.Contains(w.Body.String(), "Account ready") {
					t.Fatalf("registration: %d %s", w.Code, w.Body)
				}
				u, err := h.accounts.GetByEmail(t.Context(), "admin@gmail.com")
				if err != nil || u.NeedsVerification() || u.Password != "" {
					t.Fatalf("account: %+v %v", u, err)
				}
				for _, c := range w.Result().Cookies() {
					if c.Name == "auth-user-session" {
						t.Fatal("registration logged in without a separate login")
					}
				}
				callback = h.start("/login")
				if w := h.request("GET", callback); w.Code != 303 {
					t.Fatalf("first login: %d %s", w.Code, w.Body)
				}
			} else if mode == "link" {
				if w.Code != 303 {
					t.Fatalf("link: %d %s", w.Code, w.Body)
				}
				u, err := h.accounts.GetExternalAccount(t.Context(), user.ExternalIdentity{Issuer: issuer, Subject: "google-subject"})
				if err != nil || u.ID != account.ID || u.Password != "preserved" || u.SessionVersion != account.SessionVersion+1 {
					t.Fatalf("link: %+v %v", u, err)
				}
			} else {
				if w.Code < 400 {
					t.Fatalf("unsafe registration/link accepted: %d %s", w.Code, w.Body)
				}
				if _, err := h.accounts.GetExternalAccount(t.Context(), user.ExternalIdentity{Issuer: issuer, Subject: "google-subject"}); !errors.Is(err, user.ErrUserNotFound) {
					t.Fatal("rejected flow created identity", err)
				}
			}
		})
	}
}

func TestGoogleConcurrentCallbackIsSingleUse(t *testing.T) {
	h := newHarness(t)
	h.linkedAccount()
	callback := h.start("/login")
	var group sync.WaitGroup
	var successes atomic.Int32
	for range 4 {
		group.Go(func() {
			r := httptest.NewRequest("GET", "http://localhost"+callback, nil)
			for _, c := range h.cookies {
				r.AddCookie(c)
			}
			w := httptest.NewRecorder()
			h.handler.ServeHTTP(w, r)
			if w.Code == 303 {
				successes.Add(1)
			}
		})
	}
	group.Wait()
	if successes.Load() != 1 || h.exchanges.Load() != 1 {
		t.Fatalf("successes=%d exchanges=%d", successes.Load(), h.exchanges.Load())
	}
}

func TestGoogleLinkRequiresUnchangedVerifiedSession(t *testing.T) {
	for _, mode := range []string{"anonymous", "unverified", "revoked", "other account", "wrong identity policy", "wrong local policy"} {
		t.Run(mode, func(t *testing.T) {
			h := newHarness(t)
			account := &user.User{Email: "local@example.com", RegistrationAuthType: "email"}
			if mode == "unverified" {
				account.VerificationRequired = true
			}
			if err := h.accounts.Create(t.Context(), account); err != nil {
				t.Fatal(err)
			}
			if mode != "anonymous" {
				h.signIn(account)
			}
			if mode == "anonymous" || mode == "unverified" {
				w := h.request("POST", "/auth/link/google")
				if mode == "anonymous" && (w.Code != 303 || w.Header().Get("Location") != "/login") || mode == "unverified" && w.Code != 403 {
					t.Fatal("link started without verified login", w.Code, w.Header())
				}
				return
			}
			callback := h.start("/auth/link/google")
			switch mode {
			case "revoked":
				account.Password = "changed"
				if err := h.accounts.Update(t.Context(), account); err != nil {
					t.Fatal(err)
				}
			case "other account":
				other := &user.User{Email: "other@example.com"}
				if err := h.accounts.Create(t.Context(), other); err != nil {
					t.Fatal(err)
				}
				h.cookies = h.cookies[len(h.cookies)-1:]
				h.signIn(other)
			case "wrong identity policy":
				h.service.Policy.AllowAccount = func(_ context.Context, email string) (bool, error) { return email == account.Email, nil }
			case "wrong local policy":
				h.service.Policy.AllowAccount = func(_ context.Context, email string) (bool, error) { return email == "admin@gmail.com", nil }
			}
			if w := h.request("GET", callback); w.Code < 400 {
				t.Fatal("unsafe link accepted", w.Code)
			}
			if _, err := h.accounts.GetExternalAccount(t.Context(), user.ExternalIdentity{Issuer: issuer, Subject: "google-subject"}); !errors.Is(err, user.ErrUserNotFound) {
				t.Fatal("unsafe identity linked", err)
			}
		})
	}
}

type failedIdentityStorage struct {
	user.ExternalIdentityRepository
	operation string
}

func (s failedIdentityStorage) GetExternalAccount(ctx context.Context, id user.ExternalIdentity) (*user.User, error) {
	if s.operation == "read" {
		return nil, errors.New("storage unavailable")
	}
	return s.ExternalIdentityRepository.GetExternalAccount(ctx, id)
}

func (s failedIdentityStorage) CreateExternalAccount(context.Context, *user.User, user.ExternalIdentity) error {
	return errors.New("storage unavailable")
}

func (s failedIdentityStorage) LinkExternalIdentity(context.Context, *user.User, user.ExternalIdentity) error {
	return errors.New("storage unavailable")
}

func TestGoogleStorageFailuresDoNotLogIn(t *testing.T) {
	for _, operation := range []string{"read", "create", "link"} {
		t.Run(operation, func(t *testing.T) {
			h := newHarness(t)
			h.service.identities = failedIdentityStorage{ExternalIdentityRepository: h.accounts, operation: operation}
			path := "/register"
			if operation == "link" {
				account := &user.User{Email: "local@example.com"}
				if err := h.accounts.Create(t.Context(), account); err != nil {
					t.Fatal(err)
				}
				h.signIn(account)
				path = "/auth/link/google"
			}
			callback := h.start(path)
			w := h.request("GET", callback)
			if w.Code != 503 {
				t.Fatalf("storage outage returned %d %s", w.Code, w.Body)
			}
			for _, cookie := range w.Result().Cookies() {
				if cookie.Name == "auth-user-session" {
					t.Fatal("issued login session after failed write")
				}
			}
		})
	}
}

func TestPendingGoogleLoginCannotReplaceAnotherLogin(t *testing.T) {
	h := newHarness(t)
	h.linkedAccount()
	callback := h.start("/login")
	other := &user.User{Email: "other@example.com"}
	if err := h.accounts.Create(t.Context(), other); err != nil {
		t.Fatal(err)
	}
	h.signIn(other)
	if w := h.request("GET", callback); w.Code != 403 {
		t.Fatal("pending callback switched accounts", w.Code)
	}
	if h.exchanges.Load() != 0 {
		t.Fatal("already logged-in callback exchanged a code")
	}
}
