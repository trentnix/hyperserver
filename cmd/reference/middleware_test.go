package main

import (
	"github.com/trentnix/hyperserver/config"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func assertBrowserHeaders(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	for name, want := range map[string]string{
		"Content-Security-Policy": referenceContentSecurityPolicy,
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Referrer-Policy":         "no-referrer",
		"Permissions-Policy":      "camera=(), microphone=(), geolocation=()",
	} {
		if got := w.Result().Header.Get(name); got != want {
			t.Errorf("%s=%q, want %q", name, got, want)
		}
	}
}

// Keep these expectations independent from the application's policy constant.
// Changes to the permitted sources or restrictions must receive explicit review.
func assertReferenceCSP(t *testing.T, policy, page string) {
	t.Helper()
	const htmxSource = "/js/vendor/htmx/2.0.11/htmx.min.js"
	want := map[string]string{
		"default-src":     "'none'",
		"script-src":      "'self'",
		"style-src":       "'self' https://fonts.googleapis.com",
		"font-src":        "https://fonts.gstatic.com",
		"img-src":         "'self'",
		"connect-src":     "'self'",
		"base-uri":        "'none'",
		"object-src":      "'none'",
		"form-action":     "'self'",
		"frame-ancestors": "'none'",
	}
	for _, directive := range strings.Split(policy, ";") {
		fields := strings.Fields(directive)
		if len(fields) == 0 {
			continue
		}
		value, ok := want[fields[0]]
		if !ok || strings.Join(fields[1:], " ") != value {
			t.Errorf("unexpected or duplicate CSP directive: %q", directive)
		}
		delete(want, fields[0])
	}
	if len(want) != 0 {
		t.Errorf("missing CSP directives: %v", want)
	}
	if !strings.Contains(page, `<script src="`+htmxSource+`"`) {
		t.Error("the rendered HTMX script must use the pinned local asset")
	}
}

func TestHTTPBrowserSecurityHeaders(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		for _, htmx := range []bool{false, true} {
			for _, path := range []string{"/contact", "/auth/login/email", "/auth/register/email", "/auth/reset/request/email", "/missing", "/js/site.js"} {
				w := h.request(http.MethodGet, path, nil, htmx)
				wantStatus := http.StatusOK
				if path == "/missing" {
					wantStatus = http.StatusNotFound
				}
				if w.Code != wantStatus {
					t.Fatalf("GET %s (HTMX=%t): status=%d, want %d; body=%s", path, htmx, w.Code, wantStatus, w.Body.String())
				}
				assertBrowserHeaders(t, w)
				if w.Header().Get("Strict-Transport-Security") != "" {
					t.Fatal("HTTP response enabled HSTS")
				}
				if !htmx && path == "/contact" {
					assertReferenceCSP(t, w.Header().Get("Content-Security-Policy"), w.Body.String())
					for _, setting := range []string{`"allowEval":false`, `"allowScriptTags":false`, `"includeIndicatorStyles":false`, `"historyCacheSize":0`} {
						if !strings.Contains(w.Body.String(), setting) {
							t.Fatalf("page missing HTMX setting %s", setting)
						}
					}
				}
			}
		}
		h.baseURL = "https://127.0.0.1:8080"
		h.seedUser(t, "cookies@example.invalid")
		w := h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {"cookies@example.invalid"}, "password": {"TestPassword1!"}}, true)
		assertBrowserHeaders(t, w)
		if w.Code != 200 || w.Header().Get("HX-Redirect") == "" || w.Header().Get("Strict-Transport-Security") != "max-age=86400" {
			t.Fatalf("HTTPS login: status=%d headers=%v", w.Code, w.Header())
		}
		var found bool
		for _, cookie := range w.Result().Cookies() {
			if cookie.Name == "auth-user-session" {
				found = true
				if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Domain != "" || cookie.Path != "/" || cookie.MaxAge != 3600 {
					t.Fatalf("authentication cookie: %s", cookie)
				}
			}
		}
		if !found {
			t.Fatal("login did not set authentication cookie")
		}
	}, func(cfg *config.Config) { cfg.HTTP.HSTSMaxAge = 86400 })
}
