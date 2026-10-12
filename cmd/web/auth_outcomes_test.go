package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/trentnix/hyperserver/auth/password"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestHTTPAuthCommitsSurviveNotificationFailure(t *testing.T) {
	for _, operation := range []string{"registration", "reset"} {
		for _, htmx := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/htmx=%t", operation, htmx), func(t *testing.T) {
				runHTTPScenario(t, func(h *httpHarness) {
					path, fields := "/auth/register/email", registrationForm()
					var account *user.User
					if operation == "reset" {
						account = h.seedUser(t, "person@example.invalid")
						token, err := user.NewAuthResetToken(account, []byte(h.app.Config.Auth.JwtKey), time.Hour)
						if err != nil {
							t.Fatal(err)
						}
						if err := h.app.AccountRepository.InsertToken(t.Context(), token.Metadata()); err != nil {
							t.Fatal(err)
						}
						path = "/auth/reset/email?token=" + url.QueryEscape(token.Token)
						fields = url.Values{"password": {"NewPassword1!"}, "passwordMatch": {"NewPassword1!"}}
					}

					// Flash messages use SQLite here so a real storage failure occurs
					// after the account operation, without replacing the HTTP handlers.
					if _, err := h.app.Database.Exec(`CREATE TRIGGER reject_notification BEFORE INSERT ON session BEGIN SELECT RAISE(ABORT, 'notification storage failure'); END`); err != nil {
						t.Fatal(err)
					}
					w := h.request(http.MethodPost, path, fields, htmx)
					assertRedirectResponse(t, w, "/login", htmx)
					if w.Body.Len() != 0 || !strings.Contains(h.logs.String(), "notification storage failure") {
						t.Fatal("notification failure was not logged or caused a second response")
					}
					stored, err := h.app.AccountRepository.GetByEmail(t.Context(), "person@example.invalid")
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
						afterReplay, err := h.app.AccountRepository.GetByID(t.Context(), stored.ID)
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
	brokenRendering := func(app *application) ([]handlers.Descriptor, error) {
		// Valid syntax, but form data cannot supply this field during execution.
		files := fstest.MapFS{"failure.html": {Data: []byte(`prefix{{.Data.NonexistentField}}`)}}
		app.ContentManager.RegisterLayouts("page", files, "failure.html")
		app.ContentManager.RegisterLayouts("htmx", files, "failure.html")
		return siteModules(app, "")
	}
	for _, operation := range []string{"registration", "reset request", "change"} {
		for _, htmx := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/htmx=%t", operation, htmx), func(t *testing.T) {
				runHTTPScenarioWithCatalog(t, 30*time.Second, brokenRendering, func(h *httpHarness) {
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
					stored, err := h.app.AccountRepository.GetByEmail(t.Context(), "person@example.invalid")
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
							if _, err := user.ValidateResetToken(t.Context(), h.app.AccountRepository, link.Query().Get("token"), []byte(h.app.Config.Auth.JwtKey)); err != nil {
								t.Fatalf("delivered reset link became unusable: %v", err)
							}
						}
					}
				})
			})
		}
	}
}
