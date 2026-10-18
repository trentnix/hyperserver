package web_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/trentnix/hyperserver/auth/password"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestHTTPRecoveryRequestErrors(t *testing.T) {
	for _, operation := range []string{"reset GET", "reset POST", "verify POST"} {
		for _, scenario := range []string{"missing", "malformed", "missing stored token", "expired", "missing account", "token storage failure", "account storage failure"} {
			t.Run(operation+"/"+scenario, func(t *testing.T) {
				runSiteScenario(t, func(h *httpHarness) {
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
						if err := h.accounts.InsertToken(t.Context(), token.Metadata()); err != nil {
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
						if _, err := h.db.Exec("DELETE FROM user WHERE id = ?", u.ID); err != nil {
							t.Fatal(err)
						}
					case "token storage failure":
						table, want = "usertoken", http.StatusInternalServerError
					case "account storage failure":
						table, want = "user", http.StatusInternalServerError
					}
					if table != "" {
						if _, err := h.db.Exec("ALTER TABLE " + table + " RENAME TO unavailable"); err != nil {
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
						if _, err := h.db.Exec("ALTER TABLE unavailable RENAME TO " + table); err != nil {
							t.Fatal(err)
						}
						if _, err := h.accounts.GetToken(t.Context(), token.TokenHash, token.Type); err != nil {
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
				runSiteScenario(t, func(h *httpHarness) {
					u := h.seedUser(t, "committed@example.invalid")
					path, target := "/auth/login/email", "/"
					values := url.Values{"email": {u.Email}, "password": {"TestPassword1!"}}
					statement := `CREATE TRIGGER fail_auxiliary BEFORE INSERT ON session BEGIN SELECT RAISE(ABORT, 'private auxiliary failure'); END`
					if operation == "login notification" {
						// Allow the authenticated session, then reject the notification session.
						statement = `CREATE TRIGGER fail_auxiliary BEFORE INSERT ON session WHEN (SELECT count(*) FROM session) > 0 BEGIN SELECT RAISE(ABORT, 'private auxiliary failure'); END`
					}
					if operation == "login cleanup" {
						r := session.AddSessionManagerToRequestContext(httptest.NewRequest(http.MethodGet, h.baseURL, nil), h.sessions)
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
						if err := h.accounts.Update(t.Context(), u); err != nil {
							t.Fatal(err)
						}
						token, err := user.NewAuthVerificationToken(u, []byte(h.app.Config.Auth.JwtKey), time.Hour)
						if err != nil {
							t.Fatal(err)
						}
						if err := h.accounts.InsertToken(t.Context(), token.Metadata()); err != nil {
							t.Fatal(err)
						}
						path, values = "/auth/verify", url.Values{"token": {token.Token}}
					}
					if _, err := h.db.Exec(statement); err != nil {
						t.Fatal(err)
					}

					w := h.request(http.MethodPost, path, values, htmx)
					assertRedirectResponse(t, w, target, htmx)
					if !strings.Contains(h.logs.String(), "succeeded but") || strings.Contains(h.logs.String()+w.Body.String(), "private auxiliary failure") {
						t.Fatal("auxiliary failure was not logged safely")
					}
					stored, err := h.accounts.GetByID(t.Context(), u.ID)
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

func TestHTTPAuthCommitsSurviveNotificationFailure(t *testing.T) {
	for _, operation := range []string{"registration", "reset"} {
		for _, htmx := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/htmx=%t", operation, htmx), func(t *testing.T) {
				runSiteScenario(t, func(h *httpHarness) {
					path, fields := "/auth/register/email", registrationForm()
					var account *user.User
					if operation == "reset" {
						account = h.seedUser(t, "person@example.invalid")
						token, err := user.NewAuthResetToken(account, []byte(h.app.Config.Auth.JwtKey), time.Hour)
						if err != nil {
							t.Fatal(err)
						}
						if err := h.accounts.InsertToken(t.Context(), token.Metadata()); err != nil {
							t.Fatal(err)
						}
						path = "/auth/reset/email?token=" + url.QueryEscape(token.Token)
						fields = url.Values{"password": {"NewPassword1!"}, "passwordMatch": {"NewPassword1!"}}
					}

					// Flash messages use SQLite here so a real storage failure occurs
					// after the account operation, without replacing the HTTP handlers.
					if _, err := h.db.Exec(`CREATE TRIGGER reject_notification BEFORE INSERT ON session BEGIN SELECT RAISE(ABORT, 'notification storage failure'); END`); err != nil {
						t.Fatal(err)
					}
					w := h.request(http.MethodPost, path, fields, htmx)
					assertRedirectResponse(t, w, "/login", htmx)
					if w.Body.Len() != 0 || !strings.Contains(h.logs.String(), "notification could not be saved") || strings.Contains(h.logs.String(), "notification storage failure") {
						t.Fatal("notification failure was not logged or caused a second response")
					}
					stored, err := h.accounts.GetByEmail(t.Context(), "person@example.invalid")
					if err != nil || !password.CheckPasswordHash(fields.Get("password"), stored.Password) {
						t.Fatalf("notification failure lost the committed credentials: %v", err)
					}
					h.assertRowCount(t, "user", 1)
					if operation == "reset" {
						if stored.SessionVersion != account.SessionVersion+1 {
							t.Fatal("reset did not revoke existing sessions")
						}
						h.assertRowCount(t, "usertoken", 0)
						w = h.request(http.MethodPost, path, fields, htmx)
						if w.Code < 400 || w.Header().Get("Location") != "" || w.Header().Get("HX-Redirect") != "" {
							t.Fatal("reset token could be replayed after notification failure")
						}
						afterReplay, err := h.accounts.GetByID(t.Context(), stored.ID)
						if err != nil || *afterReplay != *stored {
							t.Fatalf("replay changed the account: %v", err)
						}
					} else if len(h.mail.snapshot()) != 1 || !stored.NeedsVerification() {
						t.Fatal("registration lost verification delivery or account state")
					}
				}, func(cfg *config.Config) { cfg.HTTP.Session.Types["hs-message-session"] = "sqliteStore" })
			})
		}
	}
}

func TestHTTPAuthCommitsSurviveRenderingFailure(t *testing.T) {
	brokenRendering := func(h *httpHarness) ([]handlers.Descriptor, error) {
		// Valid syntax, but form data cannot supply this field during execution.
		files := fstest.MapFS{"failure.html": {Data: []byte(`prefix{{.Data.NonexistentField}}`)}}
		h.app.ContentManager.RegisterLayouts("page", files, "failure.html")
		h.app.ContentManager.RegisterLayouts("htmx", files, "failure.html")
		return siteCatalog(h, "")
	}
	for _, operation := range []string{"registration", "reset request", "change"} {
		for _, htmx := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/htmx=%t", operation, htmx), func(t *testing.T) {
				runSiteScenarioWithCatalog(t, brokenRendering, func(h *httpHarness) {
					path, fields := "/auth/register/email", registrationForm()
					var account *user.User
					switch operation {
					case "registration":
						// Account creation succeeds, then delivery and the pending-account
						// response both fail. Neither failure may undo the account.
						h.mail.err = errors.New("mail delivery failure")
					case "reset request":
						account = h.seedUser(t, "person@example.invalid")
						path = "/auth/reset/request/email"
						fields = url.Values{"email": {account.Email}}
					case "change":
						account = h.seedUser(t, "person@example.invalid")
						h.establishSession(t, account)
						path = "/auth/change/email"
						fields = url.Values{"oldPassword": {"TestPassword1!"}, "newPassword": {"NewPassword1!"}, "newPasswordMatch": {"NewPassword1!"}}
					}

					w := h.request(http.MethodPost, path, fields, htmx)
					if w.Code != http.StatusInternalServerError || w.Header().Get("HX-Redirect") != "" || w.Header().Get("Location") != "" {
						t.Fatalf("render failure response: %d %v", w.Code, w.Header())
					}
					stored, err := h.accounts.GetByEmail(t.Context(), "person@example.invalid")
					if err != nil {
						t.Fatal(err)
					}
					h.assertRowCount(t, "user", 1)
					if operation == "change" {
						if stored.SessionVersion != account.SessionVersion+1 || !password.CheckPasswordHash("NewPassword1!", stored.Password) {
							t.Fatal("render failure lost the password change or session revocation")
						}
						cleared := false
						for _, cookie := range w.Result().Cookies() {
							if cookie.Name == "auth-user-session" && cookie.MaxAge == -1 {
								cleared = true
							}
						}
						if !cleared {
							t.Fatal("render failure prevented authentication cookie removal")
						}
					} else {
						h.assertRowCount(t, "usertoken", 1)
						mail := h.mail.snapshot()
						if len(mail) != 1 || mail[0].To != stored.Email {
							t.Fatal("render failure lost the stored token or mail attempt")
						}
						if operation == "registration" && !stored.NeedsVerification() {
							t.Fatal("failed registration response bypassed verification")
						}
						if operation == "reset request" {
							link := mailLink(t, mail[0], "/auth/reset/email")
							if _, err := user.ValidateResetToken(t.Context(), h.accounts, link.Query().Get("token"), []byte(h.app.Config.Auth.JwtKey)); err != nil {
								t.Fatalf("delivered reset link became unusable: %v", err)
							}
						}
					}
				})
			})
		}
	}
}
