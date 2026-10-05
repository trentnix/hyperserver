package main

import (
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func assertNoStore(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if got := w.Result().Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func assertBlankPasswordFields(t *testing.T, body string, secrets ...string) {
	t.Helper()
	inputs := regexp.MustCompile(`<input\s[^>]*type="password"[^>]*>`).FindAllString(body, -1)
	if len(inputs) == 0 {
		t.Fatal("response did not redisplay password fields")
	}
	for _, input := range inputs {
		if regexp.MustCompile(`\svalue="[^"]+"`).MatchString(input) {
			t.Error("password field redisplayed a value")
		}
	}
	for _, secret := range secrets {
		if strings.Contains(body, secret) || strings.Contains(body, html.EscapeString(secret)) {
			t.Error("response contains a submitted password")
		}
	}
}

func TestHTTPCredentialResponses(t *testing.T) {
	for _, operation := range []string{"login", "register", "reset", "change"} {
		for _, htmx := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/htmx=%t", operation, htmx), func(t *testing.T) {
				runHTTPScenario(t, func(h *httpHarness) {
					const email = "credentials@example.invalid"
					const wrong = "Wrong&A1!"
					const replacement = "Replacement&B1!"
					path := "/auth/" + operation + "/email"
					var u *user.User
					if operation != "register" {
						u = h.seedUser(t, email)
					}
					if operation == "reset" {
						token, err := user.NewAuthResetToken(u, []byte(h.app.Config.Auth.JwtKey), time.Hour)
						if err != nil {
							t.Fatal(err)
						}
						if err := token.Create(h.app.Database); err != nil {
							t.Fatal(err)
						}
						path += "?token=" + url.QueryEscape(token.Token)
					}
					if operation == "change" {
						h.establishSession(t, u)
					}
					for _, method := range []string{http.MethodGet, http.MethodHead} {
						w := h.request(method, path, nil, htmx)
						if w.Code != http.StatusOK {
							t.Fatalf("initial form: status %d", w.Code)
						}
						assertNoStore(t, w)
					}

					fields := url.Values{"email": {email}, "password": {wrong}}
					switch operation {
					case "register", "reset":
						fields.Set("passwordMatch", replacement)
					case "change":
						fields = url.Values{"oldPassword": {wrong}, "newPassword": {replacement}, "newPasswordMatch": {replacement}}
					}
					w := h.request(http.MethodPost, path, fields, htmx)
					assertNoStore(t, w)
					assertBlankPasswordFields(t, w.Body.String(), wrong, replacement)
					if operation == "login" || operation == "register" {
						if !strings.Contains(w.Body.String(), `value="`+email+`"`) {
							t.Error("validation failure lost the email address")
						}
					}

					fields.Set("password", "TestPassword1!")
					fields.Set("passwordMatch", "TestPassword1!")
					fields.Set("oldPassword", "TestPassword1!")
					w = h.request(http.MethodPost, path, fields, htmx)
					assertNoStore(t, w)
					switch operation {
					case "login":
						assertRedirectResponse(t, w, "/", htmx)
					case "register", "reset":
						assertRedirectResponse(t, w, "/login", htmx)
					case "change":
						if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Your password has been changed.") {
							t.Fatal("password change failed")
						}
						assertBlankPasswordFields(t, w.Body.String(), "TestPassword1!", replacement)
					}
				})
			})
		}
	}
}

func TestHTTPAuthCachePolicy(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		for _, htmx := range []bool{false, true} {
			for _, path := range []string{
				"/auth/login", "/auth/register", "/auth/reset/request/email",
				"/auth/login/missing", "/auth/reset/email?token=invalid", "/auth/verify?token=invalid",
				"/auth/change/email",
			} {
				assertNoStore(t, h.request(http.MethodGet, path, nil, htmx))
			}
			h.mail.err = errors.New("mail unavailable")
			fields := registrationForm()
			fields.Set("email", fmt.Sprintf("mail-failure-%t@example.invalid", htmx))
			w := h.request(http.MethodPost, "/auth/register/email", fields, htmx)
			assertNoStore(t, w)
			assertBlankPasswordFields(t, w.Body.String(), fields.Get("password"))
			if !strings.Contains(w.Body.String(), "verification email delivery failed") {
				t.Fatal("mail failure did not redisplay the form")
			}
		}
		h.establishSession(t, h.seedUser(t, "signed-in@example.invalid"))
		for _, htmx := range []bool{false, true} {
			assertNoStore(t, h.request(http.MethodGet, "/auth/login/email", nil, htmx))
		}
		// The auth policy must not disable caching for unrelated resources.
		if w := h.request(http.MethodGet, "/js/site.js", nil, false); w.Header().Get("Cache-Control") == "no-store" {
			t.Error("authentication cache policy affected static assets")
		}
	})
}

func TestHTTPDisabledRegistrationNoStore(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		for _, path := range []string{"/auth/register", "/auth/register/email"} {
			for _, method := range []string{http.MethodGet, http.MethodPost} {
				for _, htmx := range []bool{false, true} {
					w := h.request(method, path, registrationForm(), htmx)
					if w.Code != http.StatusForbidden {
						t.Fatalf("disabled registration: status %d", w.Code)
					}
					assertNoStore(t, w)
				}
			}
		}
	}, func(cfg *config.Config) { cfg.Auth.RegistrationEnabled = false })
}
