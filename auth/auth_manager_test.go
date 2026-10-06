package auth

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/auth/password"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/database"
	"github.com/trentnix/hyperserver/pkg/routing"
	content_services "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

type verificationTestAuthService struct {
	AuthService
	calls int
}

func (*verificationTestAuthService) AuthType() string { return "test-verification" }
func (*verificationTestAuthService) IsLoaded() bool   { return true }
func (s *verificationTestAuthService) SendVerificationEmail(context.Context, *user.User, url.URL) error {
	s.calls++
	return errors.New("provider error containing secret-token")
}

func TestVerificationFailureWithoutRequestLogger(t *testing.T) {
	previousServices, previousLogOutput := authServices, log.Writer()
	t.Cleanup(func() {
		authServices = previousServices
		log.SetOutput(previousLogOutput)
	})
	for _, tc := range []struct {
		name, provider, wantLog string
		wantStatus, wantCalls   int
	}{
		{"lookup failure", "", "verification account lookup failed", http.StatusInternalServerError, 0},
		{"missing provider", "missing", "verification email provider is unavailable", http.StatusServiceUnavailable, 0},
		{"delivery failure", "test-verification", "verification email delivery failed", http.StatusServiceUnavailable, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			log.SetOutput(&logs)
			sender := new(verificationTestAuthService)
			authServices = []AuthService{sender}
			manager := &AuthManager{httpConfig: config.HTTPConfig{ListenHost: "localhost", Port: 8080}, contentManager: &content_services.ContentManagerService{
				HandleError: func(w http.ResponseWriter, r *http.Request, message string, err error, status int) {
					if err != nil {
						t.Errorf("internal error reached renderer: %v", err)
					}
					http.Error(w, message, status)
				},
			}}
			r := httptest.NewRequest(http.MethodPost, "/auth/request/verify", nil)
			if tc.provider != "" {
				r = user.AddUserToRequestContext(r, &user.User{
					ID: "test-user", Email: "stored@example.invalid",
					VerificationRequired: true, RegistrationAuthType: tc.provider,
				})
			}
			w := httptest.NewRecorder()
			manager.SendVerificationRequest(w, r)

			if w.Code != tc.wantStatus || !strings.Contains(w.Body.String(), "Unable to send verification instructions.") {
				t.Fatalf("response = %d %q, want verification failure with status %d", w.Code, w.Body.String(), tc.wantStatus)
			}
			if sender.calls != tc.wantCalls {
				t.Errorf("delivery calls = %d, want %d", sender.calls, tc.wantCalls)
			}
			if !strings.Contains(logs.String(), tc.wantLog) {
				t.Errorf("logs = %q, want %q", logs.String(), tc.wantLog)
			}
			if strings.Contains(w.Body.String()+logs.String(), "secret-token") {
				t.Error("provider error leaked into the response or logs")
			}
		})
	}
}

func TestLoginRouteUsesPathRatherThanHost(t *testing.T) {
	mux := http.NewServeMux()
	manager := &AuthManager{Enabled: true}
	if err := manager.Routes(routing.NewRoutes(mux)); err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"localhost", "example.com"} {
		req := httptest.NewRequest(http.MethodGet, "http://"+host+"/auth/login", nil)
		_, pattern := mux.Handler(req)
		if pattern != "GET /auth/login" {
			t.Errorf("host %s: pattern = %q, want GET /auth/login", host, pattern)
		}
	}
}

func TestAuthRouteMethods(t *testing.T) {
	mux := http.NewServeMux()
	manager := &AuthManager{Enabled: true, registrationEnabled: true}
	if err := manager.Routes(routing.NewRoutes(mux)); err != nil {
		t.Fatal(err)
	}
	for _, route := range []struct {
		path, pattern string
		get, post     bool
	}{
		{"/auth/login", "/auth/login", true, false},
		{"/auth/login/email", "/auth/login/{authType}", true, true},
		{"/auth/logout", "/auth/logout", false, true},
		{"/auth/register", "/auth/register", true, false},
		{"/auth/register/email", "/auth/register/{authType}", true, true},
		{"/auth/verify", "/auth/verify", true, true},
		{"/auth/request/verify", "/auth/request/verify", false, true},
		{"/auth/reset/request/email", "/auth/reset/request/{authType}", true, true},
		{"/auth/reset/email", "/auth/reset/{authType}", true, true},
		{"/auth/change/email", "/auth/change/{authType}", true, true},
	} {
		for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
			t.Run(method+" "+route.path, func(t *testing.T) {
				r := httptest.NewRequest(method, route.path, nil)
				_, pattern := mux.Handler(r)
				if route.get && (method == http.MethodGet || method == http.MethodHead) {
					if pattern != "GET "+route.pattern {
						t.Fatalf("read route = %q", pattern)
					}
					return
				}
				if route.post && method == http.MethodPost {
					if pattern != "POST "+route.pattern {
						t.Fatalf("mutation route = %q", pattern)
					}
					return
				}
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, r)
				if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") == "" {
					t.Fatalf("unsupported method returned %d with Allow %q", w.Code, w.Header().Get("Allow"))
				}
			})
		}
	}
}

func TestDisabledRegistrationHandlers(t *testing.T) {
	manager := &AuthManager{Enabled: true}
	for _, handler := range []http.HandlerFunc{manager.GetRegister, manager.GetRegisterService, manager.Register} {
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest(http.MethodPost, "/auth/register/email", nil))
		if w.Code != http.StatusForbidden || strings.TrimSpace(w.Body.String()) != "Registration is not available." {
			t.Errorf("disabled registration returned %d %q, want 403 and an explanation", w.Code, w.Body.String())
		}
	}
}

func TestVerificationRejectsMissingOrMalformedForm(t *testing.T) {
	manager := &AuthManager{contentManager: &content_services.ContentManagerService{
		HandleError: func(w http.ResponseWriter, r *http.Request, message string, err error, status int) {
			http.Error(w, message, status)
		},
	}}
	for _, tc := range []struct {
		name, body, message string
	}{
		{"missing", "", "No verification token specified"},
		{"malformed", "token=%zz", "Unable to parse form data"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/auth/verify?token=query-token", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			manager.Verify(w, r)
			if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), tc.message) {
				t.Fatalf("invalid form returned %d %q", w.Code, w.Body.String())
			}
		})
	}
}

func TestRegistrationReportsDatabaseFailure(t *testing.T) {
	err, message := ProcessRegistration(context.Background(), nil, "person@example.invalid", "", "email", true)
	var unavailable *database.ErrDatabaseUnavailable
	if !errors.As(err, &unavailable) || message != "There was an error creating a user account" {
		t.Fatalf("database failure = (%v, %q), want database unavailable and creation failure", err, message)
	}
}

func TestRegistrationHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	accounts := registrationRepository{create: func(context.Context, *user.User) error {
		t.Fatal("canceled registration reached storage")
		return nil
	}}
	err, message := ProcessRegistration(ctx, accounts, "person@example.invalid", "TestPassword1!", "email", true)
	if !errors.Is(err, context.Canceled) || message != "There was an error creating a user account" {
		t.Fatalf("canceled registration = (%v, %q)", err, message)
	}
}

func TestRegistrationHashFailureDoesNotCreateAccount(t *testing.T) {
	accounts := registrationRepository{create: func(context.Context, *user.User) error {
		t.Fatal("password hashing failure reached storage")
		return nil
	}}
	err, message := ProcessRegistration(context.Background(), accounts, "person@example.invalid", strings.Repeat("a", 73), "email", true)
	if err == nil || message != "There was an error creating a user account" {
		t.Fatalf("password hashing failure = (%v, %q)", err, message)
	}
}

type registrationRepository struct {
	user.AccountRepository
	create func(context.Context, *user.User) error
}

func (s registrationRepository) Create(ctx context.Context, u *user.User) error {
	return s.create(ctx, u)
}

func TestRegistrationUsesAccountRepository(t *testing.T) {
	duplicate := errors.Join(errors.New("insert failed"), database.NewErrRecordAlreadyExists(nil))
	storageFailure := errors.New("storage unavailable")
	for _, tc := range []struct {
		name, password, message string
		failure                 error
	}{
		{name: "success", password: "TestPassword1!"},
		{name: "passwordless"},
		{name: "duplicate", failure: duplicate, message: "The specified user is already registered"},
		{name: "storage failure", failure: storageFailure, message: "There was an error creating a user account"},
		{name: "storage cancellation", failure: context.Canceled, message: "There was an error creating a user account"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			accounts := registrationRepository{create: func(got context.Context, u *user.User) error {
				calls++
				if got != ctx || u.Email != "person@example.invalid" || u.RegistrationAuthType != "email" || !u.VerificationRequired || u.Verified || u.ID != "" || !u.CreatedAt.IsZero() || !u.UpdatedAt.IsZero() || u.SessionVersion != 0 {
					t.Fatal("registration lost its context or changed the account before insertion")
				}
				if tc.password == "" {
					if u.Password != "" {
						t.Fatal("passwordless registration received a password")
					}
				} else if u.Password == tc.password || !password.CheckPasswordHash(tc.password, u.Password) {
					t.Fatal("storage did not receive a hashed password")
				}
				if tc.failure == context.Canceled {
					cancel()
					return got.Err()
				}
				return tc.failure
			}}
			err, message := ProcessRegistration(ctx, accounts, "person@example.invalid", tc.password, "email", true)
			if calls != 1 || !errors.Is(err, tc.failure) || message != tc.message {
				t.Fatalf("registration: calls=%d, error=%v, message=%q", calls, err, message)
			}
		})
	}
}

func TestDisabledAuthDoesNotRegisterLogin(t *testing.T) {
	mux := http.NewServeMux()
	manager := &AuthManager{}
	if err := manager.Routes(routing.NewRoutes(mux)); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/auth/login", nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestAuthRoutesRejectInvalidClientCapacity(t *testing.T) {
	for _, capacity := range []int{0, -1} {
		manager := &AuthManager{Enabled: true, rateLimit: &config.ModuleRateLimitConfig{MaxClients: capacity}}
		mux := http.NewServeMux()
		err := manager.Routes(routing.NewRoutes(mux))
		if err == nil || !strings.Contains(err.Error(), "auth.rateLimit.maxClients") {
			t.Fatalf("capacity %d: error = %v", capacity, err)
		}
		if _, pattern := mux.Handler(httptest.NewRequest(http.MethodGet, "/auth/login", nil)); pattern != "" {
			t.Fatal("invalid capacity left routes registered")
		}
	}
}
