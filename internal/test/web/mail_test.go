package web_test

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
)

func TestHTTPTestEmailDeliveryOutcome(t *testing.T) {
	runSiteScenario(t, func(h *httpHarness) {
		h.establishSession(t, h.seedUser(t, "mail-operator@example.invalid"))
		path := "/test-email?to=person@example.invalid"
		h.mail.err = messaging.ErrMailUnavailable
		w := h.request(http.MethodPost, path, nil, true)
		if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "failed to send test email") || strings.Contains(w.Body.String(), "test email sent to") {
			t.Fatalf("unavailable mail response: status %d, body %s", w.Code, w.Body.String())
		}
		h.mail.err = nil
		w = h.request(http.MethodPost, path, nil, true)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "test email sent to person@example.invalid") {
			t.Fatalf("accepted mail response: status %d, body %s", w.Code, w.Body.String())
		}
		if len(h.mail.snapshot()) != 2 {
			t.Fatalf("mail attempts = %d, want 2", len(h.mail.snapshot()))
		}
	})
}

func TestHTTPMailEscapesDynamicHTML(t *testing.T) {
	runSiteScenario(t, func(h *httpHarness) {
		input := `<img src=x onerror="alert(1)"> & test`
		h.app.Config.App.Name = input
		w := h.request(http.MethodPost, "/auth/register/email", registrationForm(), true)
		if w.Header().Get("HX-Redirect") != "/login" || len(h.mail.snapshot()) != 1 {
			t.Fatal("registration failed")
		}
		body := h.mail.snapshot()[0].Body
		if strings.Contains(body, "<img") || !strings.Contains(body, "&lt;img") || !strings.Contains(body, "&amp; test") {
			t.Errorf("application name was not escaped: %s", body)
		}
		if !strings.Contains(body, `<a href="http://127.0.0.1:8080/auth/verify?token=`) {
			t.Error("verification link did not remain usable")
		}
		h.establishSession(t, h.seedUser(t, "mail-operator@example.invalid"))
		w = h.request(http.MethodPost, "/test-email?to=person@example.invalid&body="+url.QueryEscape(input), nil, true)
		if w.Code != http.StatusOK || len(h.mail.snapshot()) != 2 {
			t.Fatal("test email failed")
		}
		body = h.mail.snapshot()[1].Body
		if strings.Contains(body, "<img") || !strings.Contains(body, "&lt;img") {
			t.Errorf("custom test-email text was not escaped: %s", body)
		}
	})
}

func TestHTTPAccountLinkOrigins(t *testing.T) {
	for _, tc := range []struct {
		name, origin, wantScheme, wantHost string
		secure                             bool
		port                               uint16
	}{
		{name: "direct HTTP", port: 80, wantScheme: "http", wantHost: "127.0.0.1"},
		{name: "direct HTTPS", secure: true, port: 443, wantScheme: "https", wantHost: "127.0.0.1"},
		{name: "HTTPS proxy", origin: "https://accounts.example", port: 8080, wantScheme: "https", wantHost: "accounts.example"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runSiteScenario(t, func(h *httpHarness) {
				if tc.secure {
					h.baseURL = "https://127.0.0.1"
				}
				next := h.handler
				h.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					r.Host = "attacker.example"
					r.Header.Set("X-Forwarded-Proto", "https://attacker.example/?leak=")
					r.Header.Set("X-Forwarded-Host", "attacker.example")
					r.Header.Set("Forwarded", "host=attacker.example;proto=http")
					next.ServeHTTP(w, r)
				})
				checkLink := func(index int, path string) *url.URL {
					t.Helper()
					messages := h.mail.snapshot()
					if len(messages) != index+1 {
						t.Fatalf("mail attempts = %d, want %d", len(messages), index+1)
					}
					match := regexp.MustCompile(`href="([^"]+)"`).FindStringSubmatch(messages[index].Body)
					if len(match) != 2 {
						t.Fatal("email has no link")
					}
					link, err := url.Parse(html.UnescapeString(match[1]))
					if err != nil || link.Scheme != tc.wantScheme || link.Host != tc.wantHost || link.Path != path || link.Query().Get("token") == "" {
						t.Fatalf("unexpected account link %v, error %v", link, err)
					}
					return link
				}

				h.request(http.MethodPost, "/auth/register/email", registrationForm(), true)
				checkLink(0, "/auth/verify")
				h.request(http.MethodPost, "/auth/reset/request/email", url.Values{"email": {"person@example.invalid"}}, false)
				resetLink := checkLink(1, "/auth/reset/email")
				checkResetAction := func(w *httptest.ResponseRecorder) {
					t.Helper()
					for _, attribute := range []string{"action", "hx-post"} {
						if !strings.Contains(w.Body.String(), attribute+`="`+html.EscapeString(resetLink.RequestURI())+`"`) {
							t.Errorf("%s must use a relative reset URL with the token", attribute)
						}
					}
				}
				for _, htmx := range []bool{false, true} {
					w := h.request(http.MethodGet, resetLink.RequestURI(), nil, htmx)
					checkResetAction(w)
					w = h.request(http.MethodPost, resetLink.RequestURI(), url.Values{
						"password": {"NewPassword1!"}, "passwordMatch": {"DifferentPassword1!"},
					}, htmx)
					if !strings.Contains(w.Body.String(), "Passwords do not match.") {
						t.Fatal("invalid reset did not redisplay the form")
					}
					checkResetAction(w)
				}
				h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {"person@example.invalid"}, "password": {"TestPassword1!"}}, true)
				h.request(http.MethodPost, "/auth/request/verify", nil, true)
				checkLink(2, "/auth/verify")
			}, func(cfg *config.Config) {
				cfg.HTTP.Port = tc.port
				cfg.HTTP.PublicOrigin = tc.origin
			})
		})
	}
}
