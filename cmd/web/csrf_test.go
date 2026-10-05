package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestCSRFRequestOrigins(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		called := false
		h.app.Web.HandleFunc("/csrf-probe", func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusNoContent)
		})
		for _, tc := range []struct {
			name, site, origin string
			allowed            bool
		}{
			{"same origin", "same-origin", h.baseURL, true},
			{"missing Origin", "same-origin", "", true},
			{"user initiated", "none", "", true},
			{"Origin fallback", "", h.baseURL, true},
			{"non-browser without headers", "", "", true},
			{"cross-site", "cross-site", "https://attacker.invalid", false},
			{"same-site is not same-origin", "same-site", "https://sibling.invalid", false},
			{"cross-site without Origin", "cross-site", "", false},
			{"metadata takes precedence", "cross-site", h.baseURL, false},
			{"invalid metadata", "invalid", h.baseURL, false},
			{"cross-origin fallback", "", "https://attacker.invalid", false},
			{"different port", "", "http://127.0.0.1:9000", false},
			{"null origin", "", "null", false},
			{"invalid origin", "", "https://[", false},
			{"publicOrigin is not an exemption", "cross-site", "https://public.example.invalid", false},
		} {
			for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
				for _, htmx := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/htmx=%t", tc.name, method, htmx), func(t *testing.T) {
						called = false
						r := httptest.NewRequest(method, h.baseURL+"/csrf-probe", nil)
						r.Header.Set("Origin", tc.origin)
						r.Header.Set("Sec-Fetch-Site", tc.site)
						if htmx {
							r.Header.Set("HX-Request", "true")
						}
						w := httptest.NewRecorder()
						h.handler.ServeHTTP(w, r)
						want := http.StatusForbidden
						if tc.allowed {
							want = http.StatusNoContent
						}
						if w.Code != want || called != tc.allowed {
							t.Fatalf("status=%d, handler called=%t, want %d, %t", w.Code, called, want, tc.allowed)
						}
						if w.Header().Get("X-Request-ID") == "" {
							t.Fatal("origin check ran before request logging")
						}
					})
				}
			}
		}
		// Emailed links and other safe navigation must still work across origins.
		for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
			called = false
			r := httptest.NewRequest(method, h.baseURL+"/csrf-probe", nil)
			r.Header.Set("Origin", "https://mail.example.invalid")
			r.Header.Set("Sec-Fetch-Site", "cross-site")
			w := httptest.NewRecorder()
			h.handler.ServeHTTP(w, r)
			if w.Code != http.StatusNoContent || !called {
				t.Fatalf("safe %s blocked: %d", method, w.Code)
			}
		}
	}, func(cfg *config.Config) { cfg.HTTP.PublicOrigin = "https://public.example.invalid" })
}

type csrfUnreadBody struct{ reads int }

func (b *csrfUnreadBody) Read([]byte) (int, error) {
	b.reads++
	return 0, io.EOF
}

func (*csrfUnreadBody) Close() error { return nil }

func TestCSRFRejectsBeforeSessionLoading(t *testing.T) {
	// Missing storage and session services would fail if CSRF checks ran too late.
	app := &server.ApplicationServer{Config: &config.Config{}, Web: http.NewServeMux()}
	app.Web.HandleFunc("POST /mutation", func(http.ResponseWriter, *http.Request) {
		t.Fatal("rejected request reached its handler")
	})
	logs := &capturedLogs{}
	handler, err := applicationHandler(app, &testLogger{logs: logs})
	if err != nil {
		t.Fatal(err)
	}
	body := &csrfUnreadBody{}
	r := httptest.NewRequest(http.MethodPost, "http://localhost/mutation", body)
	r.Header.Set("Origin", "https://attacker.invalid")
	r.Header.Set("HX-Request", "true")
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// Neither forwarded headers nor an invalid session cookie must bypass the check.
	r.Header.Set("X-Forwarded-Host", "attacker.invalid")
	r.Header.Set("X-Forwarded-Proto", "https")
	r.AddCookie(&http.Cookie{Name: "auth-user-session", Value: "invalid-token"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden || body.reads != 0 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("rejection touched request state: status=%d, body reads=%d, cookies=%v", w.Code, body.reads, w.Result().Cookies())
	}
	if w.Header().Get("X-Request-ID") == "" || !strings.Contains(logs.String(), "Incoming request") {
		t.Fatal("rejected request was not logged")
	}
}

func TestCSRFCrossOriginMutationsHaveNoSideEffects(t *testing.T) {
	for _, authenticated := range []bool{false, true} {
		t.Run(fmt.Sprintf("authenticated=%t", authenticated), func(t *testing.T) {
			runHTTPScenario(t, func(h *httpHarness) {
				u := h.seedUser(t, "csrf@example.invalid")
				verification, err := user.NewAuthVerificationToken(u, []byte(h.app.Config.Auth.JwtKey), time.Hour)
				if err != nil {
					t.Fatal(err)
				}
				if err := verification.Create(h.app.Database); err != nil {
					t.Fatal(err)
				}
				reset, err := user.NewAuthResetToken(u, []byte(h.app.Config.Auth.JwtKey), time.Hour)
				if err != nil {
					t.Fatal(err)
				}
				if err := reset.Create(h.app.Database); err != nil {
					t.Fatal(err)
				}
				wantSessions := 0
				if authenticated {
					h.establishSession(t, u)
					wantSessions = 1
				}
				base, _ := url.Parse(h.baseURL)
				for _, htmx := range []bool{false, true} {
					for _, tc := range []struct {
						path string
						form url.Values
					}{
						{"/auth/register/email", registrationForm()},
						{"/auth/login/email", url.Values{"email": {u.Email}, "password": {"TestPassword1!"}}},
						{"/auth/logout", nil},
						{"/logout", nil},
						{"/auth/request/verify", nil},
						{"/auth/verify", url.Values{"token": {verification.Token}}},
						{"/auth/reset/request/email", url.Values{"email": {u.Email}}},
						{"/auth/reset/email?token=" + url.QueryEscape(reset.Token), url.Values{"password": {"NewPassword1!"}, "passwordMatch": {"NewPassword1!"}}},
						{"/auth/change/email", url.Values{"oldPassword": {"TestPassword1!"}, "newPassword": {"NewPassword1!"}, "newPasswordMatch": {"NewPassword1!"}}},
						{"/contact", url.Values{"name": {"Person"}, "email": {u.Email}, "message": {"forged message"}}},
						{"/test-email?to=" + url.QueryEscape(u.Email), nil},
						{"/session-example", nil},
					} {
						r := httptest.NewRequest(http.MethodPost, h.baseURL+tc.path, strings.NewReader(tc.form.Encode()))
						r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
						r.Header.Set("Origin", "https://attacker.invalid")
						r.Header.Set("Sec-Fetch-Site", "cross-site")
						if htmx {
							r.Header.Set("HX-Request", "true")
						}
						// Include cookies explicitly rather than relying on SameSite protection.
						for _, cookie := range h.cookies.Cookies(base) {
							r.AddCookie(cookie)
						}
						w := httptest.NewRecorder()
						h.handler.ServeHTTP(w, r)
						if w.Code != http.StatusForbidden || len(w.Result().Cookies()) != 0 || w.Header().Get("Location") != "" || w.Header().Get("HX-Redirect") != "" {
							t.Fatalf("%s was not rejected cleanly: %d %v", tc.path, w.Code, w.Header())
						}
						h.assertUserUnchanged(t, u)
						h.assertRowCount(t, "user", 1)
						h.assertRowCount(t, "usertoken", 2)
						h.assertRowCount(t, "session", wantSessions)
						h.assertRowCount(t, "hyperserver_contact_submission", 0)
						if len(h.mail.snapshot()) != 0 {
							t.Fatalf("%s sent mail", tc.path)
						}
					}
				}
			})
		})
	}
}
