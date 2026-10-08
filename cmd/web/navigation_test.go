package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/session"
)

func TestHTTPNavigationLogout(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(fmt.Sprintf("pending=%t", pending), func(t *testing.T) {
			runHTTPScenario(t, func(h *httpHarness) {
				for _, path := range []string{"/", "/login", "/auth/login/email"} {
					w := h.request(http.MethodGet, path, nil, false)
					if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `<a href="/login">Log in</a>`) || strings.Contains(w.Body.String(), `action="/auth/logout"`) {
						t.Fatalf("%s did not show only the login action to a signed-out visitor", path)
					}
				}
				u := h.seedUser(t, "navigation@example.invalid")
				u.Verified, u.VerificationRequired = !pending, true
				if err := h.app.AccountRepository.Update(t.Context(), u); err != nil {
					t.Fatal(err)
				}
				h.establishSession(t, u)
				paths := []string{"/"}
				if pending {
					paths = append(paths, "/auth/request/verify")
				}
				for _, path := range paths {
					w := h.request(http.MethodGet, path, nil, false)
					nav := regexp.MustCompile(`(?s)<nav id="navbar">.*?</nav>`).FindString(w.Body.String())
					if w.Code != http.StatusOK || !strings.Contains(nav, `<form class="logout-form" method="post" action="/auth/logout">`) || !strings.Contains(nav, `<button type="submit">Log out</button>`) {
						t.Fatalf("%s is missing the native navigation logout form", path)
					}
					if strings.Contains(nav, ">Log in</a>") {
						t.Fatal("signed-in navigation still offered login")
					}
				}

				w := h.request(http.MethodPost, "/auth/logout", nil, false)
				assertRedirectResponse(t, w, "/", false)
				h.assertRowCount(t, "session", 0)
				w = h.request(http.MethodGet, "/", nil, false)
				if w.Code != http.StatusOK || strings.Contains(w.Body.String(), u.Email) || !strings.Contains(w.Body.String(), ">Log in</a>") || strings.Contains(w.Body.String(), `action="/auth/logout"`) {
					t.Fatal("logout did not return to an anonymous home page")
				}
			})
		})
	}
}

func TestSiteNavigationWithoutAuthentication(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		// Test the site's navigation without the reference application's
		// authentication middleware, which currently requires an account repository.
		r := session.AddSessionManagerToRequestContext(httptest.NewRequest(http.MethodGet, "/", nil), h.app.SessionManager)
		w := httptest.NewRecorder()
		h.app.Web.ServeHTTP(w, r)
		if w.Code != http.StatusOK || strings.Contains(w.Body.String(), ">Log in</a>") || strings.Contains(w.Body.String(), `action="/auth/logout"`) {
			t.Fatalf("disabled authentication: status %d, body %s", w.Code, w.Body.String())
		}
	}, func(cfg *config.Config) { cfg.Auth.Enabled = false })
}
