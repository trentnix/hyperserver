package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/server"
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
	const htmxSource = "https://unpkg.com/htmx.org@2.0.1"
	want := map[string]string{
		"default-src":     "'none'",
		"script-src":      "'self' " + htmxSource,
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
		t.Error("the rendered HTMX script must match the CSP's pinned source")
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

func TestSecurityHeadersPrecedeRejections(t *testing.T) {
	app := &server.ApplicationServer{Config: &config.Config{}, Web: http.NewServeMux()}
	configureTestProxy(app.Config)
	app.Config.HTTP.HSTSMaxAge = 86400
	app.Config.HTTP.SharedRateLimit = config.RateLimitConfig{Enabled: true, Requests: 2, Window: time.Minute, MaxClients: 10}
	handler, err := applicationHandler(app, &testLogger{logs: &capturedLogs{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []int{400, 403, 500, 429} {
		r := proxyRequest(http.MethodPost, "/", "203.0.113.1", nil)
		switch status {
		case 400:
			r.Header.Del("X-Forwarded-Proto")
		case 403:
			r.Header.Set("Origin", "https://attacker.test")
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("status=%d, want %d", w.Code, status)
		}
		assertBrowserHeaders(t, w)
		wantHSTS := "max-age=86400"
		if status == 400 {
			wantHSTS = ""
		}
		if w.Header().Get("Strict-Transport-Security") != wantHSTS {
			t.Fatalf("unverified or missing HSTS: %v", w.Header())
		}
	}
	app.Config.HTTP.HSTSMaxAge = -1
	if handler, err := applicationHandler(app, &testLogger{logs: &capturedLogs{}}); handler != nil || err == nil {
		t.Fatal("negative HSTS configuration accepted")
	}
}
