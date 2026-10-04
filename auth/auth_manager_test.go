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

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/database"
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
	manager.Routes(mux)
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
	manager.Routes(mux)
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
		{"malformed", "token=%zz", "Unable to read the verification form"},
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
	err, message := ProcessRegistration(ctx, nil, "person@example.invalid", "TestPassword1!", "email", true)
	if !errors.Is(err, context.Canceled) || message != "There was an error creating a user account" {
		t.Fatalf("canceled registration = (%v, %q)", err, message)
	}
}

func TestDisabledAuthDoesNotRegisterLogin(t *testing.T) {
	mux := http.NewServeMux()
	manager := &AuthManager{}
	manager.Routes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/auth/login", nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}
