package web_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	authmiddleware "github.com/trentnix/hyperserver/auth/middleware"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/middleware"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestHTTPAuthorization(t *testing.T) {
	runSiteScenario(t, func(h *httpHarness) {
		owner := h.seedUser(t, "owner@example.invalid")
		other := h.seedUser(t, "other@example.invalid")
		policyCalls, handlerCalls := 0, 0
		permission := true
		var policyErr error
		policy := func(r *http.Request) (bool, error) {
			policyCalls++
			u := user.GetUserFromContext(r.Context())
			return permission && u != nil && !u.NeedsVerification() && u.ID == r.PathValue("owner"), policyErr
		}
		handler := middleware.RequireAuthorization(policy)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handlerCalls++
			w.WriteHeader(http.StatusNoContent)
		}))
		h.app.Web.Handle("GET /authorization/{owner}", handler)
		h.app.Web.Handle("POST /authorization/{owner}", handler)

		check := func(want int) {
			t.Helper()
			for _, method := range []string{http.MethodGet, http.MethodPost} {
				for _, htmx := range []bool{false, true} {
					policyCalls, handlerCalls = 0, 0
					w := h.request(method, "/authorization/"+owner.ID, nil, htmx)
					wantHandlerCalls := 0
					if want == http.StatusNoContent {
						wantHandlerCalls = 1
					}
					if w.Code != want || policyCalls != 1 || handlerCalls != wantHandlerCalls {
						t.Fatalf("%s htmx=%t: status=%d policy calls=%d handler calls=%d, want %d 1 %d", method, htmx, w.Code, policyCalls, handlerCalls, want, wantHandlerCalls)
					}
					if strings.Contains(w.Body.String(), "private") || len(w.Result().Cookies()) != 0 || w.Header().Get("Location") != "" || w.Header().Get("HX-Redirect") != "" {
						t.Fatalf("unexpected authorization response: %v %s", w.Header(), w.Body.String())
					}
				}
			}
		}

		check(http.StatusForbidden) // Anonymous requests do not satisfy this policy.
		h.establishSession(t, other)
		check(http.StatusForbidden) // Authentication alone does not grant access.
		h.establishSession(t, owner)
		check(http.StatusNoContent)

		permission = false
		check(http.StatusForbidden) // Reevaluate policy with the same session.
		permission = true

		policyErr = errors.New("private authorization storage failure")
		check(http.StatusInternalServerError)
		if !strings.Contains(h.logs.String(), policyErr.Error()) {
			t.Fatal("policy failure was not logged")
		}
		policyErr = nil
		check(http.StatusNoContent)

		owner.Verified, owner.VerificationRequired = false, true
		if err := h.accounts.Update(context.Background(), owner); err != nil {
			t.Fatal(err)
		}
		// Account-security changes revoke existing sessions.
		h.establishSession(t, owner)
		check(http.StatusForbidden) // This policy also requires account verification.
		owner.Verified = true
		if err := h.accounts.Update(context.Background(), owner); err != nil {
			t.Fatal(err)
		}
		h.establishSession(t, owner)
		check(http.StatusNoContent)

		// Identity-loading failures must stop before authorization or handler work.
		if _, err := h.db.Exec(`ALTER TABLE user RENAME TO unavailable_user`); err != nil {
			t.Fatal(err)
		}
		for _, htmx := range []bool{false, true} {
			policyCalls, handlerCalls = 0, 0
			w := h.request(http.MethodPost, "/authorization/"+owner.ID, nil, htmx)
			if w.Code != http.StatusInternalServerError || policyCalls != 0 || handlerCalls != 0 {
				t.Fatalf("identity-loading failure reached authorization: status=%d policy calls=%d handler calls=%d", w.Code, policyCalls, handlerCalls)
			}
		}
	})
}

func TestHTTPAuthenticationRedirectSaveFailure(t *testing.T) {
	for _, htmx := range []bool{false, true} {
		name := "navigation"
		if htmx {
			name = "htmx"
		}
		t.Run(name, func(t *testing.T) {
			runSiteScenario(t, func(h *httpHarness) {
				// Reads and initialization work. Only saving the redirect session fails.
				if _, err := h.db.Exec(`CREATE TRIGGER reject_redirect BEFORE INSERT ON session BEGIN SELECT RAISE(ABORT, 'redirect write failed'); END`); err != nil {
					t.Fatal(err)
				}
				h.app.Web.Handle("GET /protected-save-check", authmiddleware.RequireAuthentication(h.accounts, h.app.ContentManager)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
					t.Error("anonymous request reached the protected handler")
				})))
				w := h.request(http.MethodGet, "/protected-save-check", nil, htmx)
				if w.Code != http.StatusInternalServerError || w.Body.String() != "Unable to save the login session\n" {
					t.Fatalf("save failure response = %d %q", w.Code, w.Body.String())
				}
				if w.Header().Get("HX-Redirect") != "" || w.Header().Get("Location") != "" || len(w.Result().Cookies()) != 0 {
					t.Fatal("failed save redirected or wrote cookies")
				}
				if !strings.Contains(h.logs.String(), "redirect write failed") {
					t.Fatal("save failure was not logged")
				}
				h.assertRowCount(t, "session", 0)

				if _, err := h.db.Exec(`DROP TRIGGER reject_redirect`); err != nil {
					t.Fatal(err)
				}
				h.logs.mu.Lock()
				h.logs.lines = nil
				h.logs.mu.Unlock()
				w = h.request(http.MethodGet, "/protected-save-check", nil, htmx)
				assertRedirectResponse(t, w, "/login", htmx)
				if strings.Contains(h.logs.String(), "Request Error") {
					t.Fatalf("successful save logged an error: %s", h.logs.String())
				}
				h.assertRowCount(t, "session", 1)
			}, func(cfg *config.Config) {
				useSQLiteSessions(cfg)
				cfg.HTTP.Session.Stores["cookieStore"] = map[string]string{"enabled": "true"}
				cfg.HTTP.Session.Types["default"] = "cookieStore"
				cfg.HTTP.Session.Types[session.AuthSession] = "sqliteStore"
			})
		})
	}
}

func TestHTTPTestEmailAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		authenticated        bool
		verified             bool
		verificationRequired bool
		status               int
	}{
		{"anonymous", false, false, false, http.StatusForbidden},
		{"pending verification", true, false, true, http.StatusForbidden},
		{"verified", true, true, true, http.StatusOK},
		{"verification not required", true, false, false, http.StatusOK},
		{"revoked session", true, true, true, http.StatusForbidden},
		{"identity storage failure", true, true, true, http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runSiteScenario(t, func(h *httpHarness) {
				if tc.authenticated {
					u := h.seedUser(t, "person@example.invalid")
					u.Verified, u.VerificationRequired = tc.verified, tc.verificationRequired
					if err := h.accounts.Update(context.Background(), u); err != nil {
						t.Fatal(err)
					}
					h.establishSession(t, u)
					if tc.name == "revoked session" {
						if _, err := h.db.Exec(`UPDATE user SET session_version = session_version + 1 WHERE id = ?`, u.ID); err != nil {
							t.Fatal(err)
						}
					}
					if tc.name == "identity storage failure" {
						if _, err := h.db.Exec(`ALTER TABLE user RENAME TO unavailable_user`); err != nil {
							t.Fatal(err)
						}
					}
				}

				for _, htmx := range []bool{false, true} {
					for _, path := range []string{"/test-email?to=recipient@example.invalid", "/test-email"} {
						before := len(h.mail.snapshot())
						wantStatus, wantMail := tc.status, before
						if tc.status == http.StatusOK {
							if path == "/test-email" {
								wantStatus = http.StatusBadRequest
							} else {
								wantMail++
							}
						}
						w := h.request(http.MethodPost, path, nil, htmx)
						mail := h.mail.snapshot()
						if w.Code != wantStatus || len(mail) != wantMail {
							t.Fatalf("%s htmx=%t: status=%d mail=%d, want %d %d", path, htmx, w.Code, len(mail), wantStatus, wantMail)
						}
						if wantMail > before && mail[before].To != "recipient@example.invalid" {
							t.Fatalf("wrong mail recipient: %q", mail[before].To)
						}
						if strings.Contains(w.Body.String(), "unavailable_user") || len(w.Result().Cookies()) != 0 || w.Header().Get("Location") != "" || w.Header().Get("HX-Redirect") != "" {
							t.Fatalf("unexpected response: %v %s", w.Header(), w.Body.String())
						}
					}
				}
			})
		})
	}
}
