package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestHTTPRecoveryRequestErrors(t *testing.T) {
	for _, operation := range []string{"reset GET", "reset POST", "verify POST"} {
		for _, scenario := range []string{"missing", "malformed", "missing stored token", "expired", "missing account", "token storage failure", "account storage failure"} {
			t.Run(operation+"/"+scenario, func(t *testing.T) {
				runHTTPScenario(t, func(h *httpHarness) {
					u := h.seedUser(t, "recovery@example.invalid")
					key := []byte(h.app.Config.Auth.JwtKey)
					reset, err := user.NewAuthResetToken(u, key, time.Hour)
					if err != nil {
						t.Fatal(err)
					}
					token := reset.AuthToken
					if operation == "verify POST" {
						verification, err := user.NewAuthVerificationToken(u, key, time.Hour)
						if err != nil {
							t.Fatal(err)
						}
						token = verification.AuthToken
					}
					if scenario == "expired" {
						token.ExpiresAt = time.Now().Add(-time.Hour)
					}
					if scenario != "missing stored token" {
						if err := h.app.AccountRepository.InsertToken(t.Context(), token.Metadata()); err != nil {
							t.Fatal(err)
						}
					}

					presented := token.Token
					want := http.StatusBadRequest
					table := ""
					switch scenario {
					case "missing":
						presented = ""
					case "malformed":
						presented = "private-invalid-token"
					case "missing account":
						if _, err := h.app.Database.Exec("DELETE FROM user WHERE id = ?", u.ID); err != nil {
							t.Fatal(err)
						}
					case "token storage failure":
						table, want = "usertoken", http.StatusInternalServerError
					case "account storage failure":
						table, want = "user", http.StatusInternalServerError
					}
					if table != "" {
						if _, err := h.app.Database.Exec("ALTER TABLE " + table + " RENAME TO unavailable"); err != nil {
							t.Fatal(err)
						}
					}

					for _, htmx := range []bool{false, true} {
						path := "/auth/reset/email?token=" + url.QueryEscape(presented)
						method := http.MethodPost
						values := url.Values{"password": {"NewPassword1!"}, "passwordMatch": {"NewPassword1!"}}
						if operation == "reset GET" {
							method = http.MethodGet
						} else if operation == "verify POST" {
							path, values = "/auth/verify", url.Values{"token": {presented}}
						}
						w := h.request(method, path, values, htmx)
						if w.Code != want || w.Header().Get("HX-Redirect") != "" || w.Header().Get("Location") != "" {
							t.Fatalf("htmx=%t: status=%d headers=%v body=%s", htmx, w.Code, w.Header(), w.Body.String())
						}
						h.assertNoTokenExposure(t, w, token.Token)
						for _, secret := range []string{"private-invalid-token", "NewPassword1!", "no such table"} {
							if strings.Contains(w.Body.String()+h.logs.String(), secret) {
								t.Fatalf("response or logs exposed %q", secret)
							}
						}
						if want == http.StatusInternalServerError && !strings.Contains(h.logs.String(), "recovery operation failed") {
							t.Fatal("infrastructure failure lost its sanitized diagnostic")
						}
					}
					if table != "" {
						if _, err := h.app.Database.Exec("ALTER TABLE unavailable RENAME TO " + table); err != nil {
							t.Fatal(err)
						}
						if _, err := h.app.AccountRepository.GetToken(t.Context(), token.TokenHash, token.Type); err != nil {
							t.Fatalf("storage failure consumed the token: %v", err)
						}
					}
					if scenario != "missing account" {
						h.assertUserUnchanged(t, u)
					}
				})
			})
		}
	}
}

func TestHTTPCommittedChangesSurviveAuxiliaryFailures(t *testing.T) {
	for _, operation := range []string{"login notification", "verification notification", "login cleanup"} {
		for _, htmx := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/htmx=%t", operation, htmx), func(t *testing.T) {
				runHTTPScenario(t, func(h *httpHarness) {
					u := h.seedUser(t, "committed@example.invalid")
					path, target := "/auth/login/email", "/"
					values := url.Values{"email": {u.Email}, "password": {"TestPassword1!"}}
					statement := `CREATE TRIGGER fail_auxiliary BEFORE INSERT ON session BEGIN SELECT RAISE(ABORT, 'private auxiliary failure'); END`
					if operation == "login notification" {
						// Allow the authenticated session, then reject the notification session.
						statement = `CREATE TRIGGER fail_auxiliary BEFORE INSERT ON session WHEN (SELECT count(*) FROM session) > 0 BEGIN SELECT RAISE(ABORT, 'private auxiliary failure'); END`
					}
					if operation == "login cleanup" {
						r := session.AddSessionManagerToRequestContext(httptest.NewRequest(http.MethodGet, h.baseURL, nil), h.app.SessionManager)
						s, err := session.New(r, session.AuthSession)
						if err != nil {
							t.Fatal(err)
						}
						s.Data[session.RedirectURL] = "/about"
						w := httptest.NewRecorder()
						if err := s.Save(w, r); err != nil {
							t.Fatal(err)
						}
						h.cookies.SetCookies(r.URL, w.Result().Cookies())
						statement = fmt.Sprintf(`CREATE TRIGGER fail_auxiliary BEFORE DELETE ON session WHEN OLD.id = '%s' BEGIN SELECT RAISE(ABORT, 'private auxiliary failure'); END`, s.ID)
						target = "/about"
					}
					if operation == "verification notification" {
						u.Verified = false
						if err := h.app.AccountRepository.Update(t.Context(), u); err != nil {
							t.Fatal(err)
						}
						token, err := user.NewAuthVerificationToken(u, []byte(h.app.Config.Auth.JwtKey), time.Hour)
						if err != nil {
							t.Fatal(err)
						}
						if err := h.app.AccountRepository.InsertToken(t.Context(), token.Metadata()); err != nil {
							t.Fatal(err)
						}
						path, values = "/auth/verify", url.Values{"token": {token.Token}}
					}
					if _, err := h.app.Database.Exec(statement); err != nil {
						t.Fatal(err)
					}

					w := h.request(http.MethodPost, path, values, htmx)
					assertRedirectResponse(t, w, target, htmx)
					if !strings.Contains(h.logs.String(), "succeeded but") || strings.Contains(h.logs.String()+w.Body.String(), "private auxiliary failure") {
						t.Fatal("auxiliary failure was not logged safely")
					}
					stored, err := h.app.AccountRepository.GetByID(t.Context(), u.ID)
					if err != nil {
						t.Fatal(err)
					}
					if operation == "verification notification" && !stored.Verified {
						t.Fatal("notification failure lost verification")
					}
					h.assertRowCount(t, "usertoken", 0)
					if strings.HasPrefix(operation, "login") {
						h.authenticationCookie(t)
						wantSessions := 1
						if operation == "login cleanup" {
							wantSessions = 3 // Authentication, notification, and the uncleared redirect.
						}
						h.assertRowCount(t, "session", wantSessions)
					}
				}, func(cfg *config.Config) {
					cfg.HTTP.Session.Types["hs-message-session"] = "sqliteStore"
					cfg.HTTP.Session.Types[session.AuthSession] = "sqliteStore"
				})
			})
		}
	}
}
