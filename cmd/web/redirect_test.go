package main

import (
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/pkg/services/middleware"
	"github.com/trentnix/hyperserver/pkg/services/session"
)

func assertRedirectResponse(t *testing.T, w *httptest.ResponseRecorder, destination string, htmx bool) {
	t.Helper()
	status, header, absent := http.StatusSeeOther, "Location", "HX-Redirect"
	if htmx {
		status, header, absent = http.StatusOK, "HX-Redirect", "Location"
	}
	if w.Code != status || w.Header().Get(header) != destination || w.Header().Get(absent) != "" || w.Body.Len() != 0 {
		t.Fatalf("redirect: status=%d headers=%v body=%q, want %d %s=%q", w.Code, w.Header(), w.Body.String(), status, header, destination)
	}
}

func TestHTTPLoginRedirectDestinations(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		u := h.seedUser(t, "redirect@example.invalid")
		for _, htmx := range []bool{false, true} {
			for _, tc := range []struct{ destination, home, want string }{
				{"", "/welcome", "/welcome"},
				{"/contact?from=login#form", "/welcome", "/contact?from=login#form"},
				{"https://attacker.invalid", "/welcome", "/welcome"},
				{h.baseURL + "/contact", "/welcome", "/welcome"},
				{"//attacker.invalid", "/welcome", "/welcome"},
				{`/\attacker.invalid`, "/welcome", "/welcome"},
				{"/page?x=%0a", "/welcome", "/welcome"},
				{"/bad%", "/welcome", "/welcome"},
				{"//attacker.invalid", "https://attacker.invalid", "/"},
			} {
				// Each login starts with a fresh browser, without a prior account session.
				jar, err := cookiejar.New(nil)
				if err != nil {
					t.Fatal(err)
				}
				h.cookies = jar
				r := session.AddSessionManagerToRequestContext(httptest.NewRequest(http.MethodGet, "/", nil), h.app.SessionManager)
				s, err := session.New(r, session.AuthSession)
				if err != nil {
					t.Fatal(err)
				}
				s.Data[session.RedirectURL] = tc.destination
				w := httptest.NewRecorder()
				if err := s.Save(w, r); err != nil {
					t.Fatal(err)
				}
				base, _ := url.Parse(h.baseURL)
				h.cookies.SetCookies(base, w.Result().Cookies())

				h.app.ContentManager.HomeURL = tc.home
				w = h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {u.Email}, "password": {"TestPassword1!"}}, htmx)
				assertRedirectResponse(t, w, tc.want, htmx)
				for _, cookie := range h.cookies.Cookies(base) {
					if cookie.Name == session.AuthSession {
						t.Fatal("login did not clear the return destination session")
					}
				}
				if home := h.request(http.MethodGet, "/", nil, false); !strings.Contains(home.Body.String(), u.Email) {
					t.Fatal("redirect selection lost the authenticated session")
				}
			}
		}
	})
}

func TestHTTPLoginReturnsToRequestedPath(t *testing.T) {
	for _, htmx := range []bool{false, true} {
		name := "ordinary"
		if htmx {
			name = "htmx"
		}
		t.Run(name, func(t *testing.T) {
			runHTTPScenario(t, func(h *httpHarness) {
				u := h.seedUser(t, "return@example.invalid")
				called := false
				h.app.Web.Handle("GET /protected-return", middleware.RequireAuthentication(h.app.AccountRepository, h.app.ContentManager)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					called = true
					w.WriteHeader(http.StatusNoContent)
				})))
				// The harness uses absolute-form request targets. Save only path and query.
				path := "/protected-return?tab=settings"
				w := h.request(http.MethodGet, path, nil, htmx)
				assertRedirectResponse(t, w, "/login", htmx)
				if called {
					t.Fatal("anonymous request reached the protected handler")
				}
				w = h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {u.Email}, "password": {"TestPassword1!"}}, htmx)
				assertRedirectResponse(t, w, path, htmx)
				w = h.request(http.MethodGet, path, nil, false)
				if w.Code != http.StatusNoContent || !called {
					t.Fatal("login did not grant access to the original destination")
				}
			})
		})
	}
}
