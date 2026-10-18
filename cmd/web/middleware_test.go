package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/server"
)

func TestGlobalRateLimitPrecedesSessionLoading(t *testing.T) {
	app := &application{ApplicationServer: &server.ApplicationServer{Config: &config.Config{}, Web: http.NewServeMux()}}
	app.Config.HTTP.SharedRateLimit = config.RateLimitConfig{Enabled: true, Requests: 1, Window: time.Minute, MaxClients: 10}
	logs := &capturedLogs{}
	handler, err := applicationHandler(app, newTestLogger(logs))
	if err != nil {
		t.Fatal(err)
	}
	// The first request is admitted and fails because there are no session services.
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != 500 {
		t.Fatalf("initial status=%d", w.Code)
	}

	body := &csrfUnreadBody{}
	r := httptest.NewRequest(http.MethodPost, "/", body)
	r.AddCookie(&http.Cookie{Name: "auth-user-session", Value: "invalid-token"})
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 429 || body.reads != 0 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("rejection touched request state: status=%d reads=%d cookies=%v", w.Code, body.reads, w.Result().Cookies())
	}
	if w.Header().Get("X-Request-ID") == "" || !strings.Contains(logs.String(), "Request completed") {
		t.Fatal("rejection was not logged")
	}

	// A separately composed application must not inherit an exhausted budget.
	other, err := applicationHandler(app, newTestLogger(logs))
	if err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	other.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != 500 {
		t.Fatal("independent handler inherited the global limit")
	}
}

func TestApplicationHandlerRejectsInvalidRateLimit(t *testing.T) {
	for _, limit := range []config.RateLimitConfig{
		{Enabled: true, Window: time.Minute, MaxClients: 10},
		{Enabled: true, Requests: 1, MaxClients: 10},
		{Enabled: true, Requests: 1, Window: time.Minute},
	} {
		app := &application{ApplicationServer: &server.ApplicationServer{Config: &config.Config{HTTP: config.HTTPConfig{SharedRateLimit: limit}}, Web: http.NewServeMux()}}
		handler, err := applicationHandler(app, newTestLogger(&capturedLogs{}))
		if handler != nil || err == nil || !strings.Contains(err.Error(), "http.sharedRateLimit") {
			t.Fatalf("handler=%v error=%v", handler, err)
		}
	}
}

func proxyRequest(method, path, client string, form url.Values) *http.Request {
	r := httptest.NewRequest(method, "http://example.test"+path, strings.NewReader(form.Encode()))
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("X-Forwarded-For", client)
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("Origin", "https://example.test")
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

func configureTestProxy(cfg *config.Config) {
	cfg.HTTP.TrustedProxies = []string{"192.0.2.1/32"}
	cfg.HTTP.PublicOrigin = "https://example.test"
}

func TestInvalidProxyMetadataPrecedesApplicationWork(t *testing.T) {
	app := &application{ApplicationServer: &server.ApplicationServer{Config: &config.Config{}, Web: http.NewServeMux()}}
	configureTestProxy(app.Config)
	app.Config.HTTP.SharedRateLimit = config.RateLimitConfig{Enabled: true, Requests: 1, Window: time.Minute, MaxClients: 10}
	handler, err := applicationHandler(app, newTestLogger(&capturedLogs{}))
	if err != nil {
		t.Fatal(err)
	}
	body := &csrfUnreadBody{}
	r := proxyRequest(http.MethodPost, "/", "203.0.113.1", nil)
	r.Header.Del("X-Forwarded-Proto")
	r.Body = body
	r.AddCookie(&http.Cookie{Name: "auth-user-session", Value: "invalid-token"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 400 || body.reads != 0 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("invalid metadata: status=%d body reads=%d cookies=%v", w.Code, body.reads, w.Result().Cookies())
	}

	// Invalid metadata must not consume capacity or reach the missing session service.
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, proxyRequest(http.MethodGet, "/", "203.0.113.1", nil))
	if w.Code != 500 {
		t.Fatalf("valid metadata: status=%d, want session initialization failure", w.Code)
	}
	app.Config.HTTP.TrustedProxies = []string{"invalid"}
	if handler, err := applicationHandler(app, newTestLogger(&capturedLogs{})); err == nil || handler != nil {
		t.Fatal("invalid proxy configuration accepted")
	}
}

type csrfUnreadBody struct{ reads int }

func (b *csrfUnreadBody) Read([]byte) (int, error) {
	b.reads++
	return 0, io.EOF
}

func (*csrfUnreadBody) Close() error { return nil }

func TestCSRFRejectsBeforeSessionLoading(t *testing.T) {
	// Missing storage and session services would fail if CSRF checks ran too late.
	app := &application{ApplicationServer: &server.ApplicationServer{Config: &config.Config{}, Web: http.NewServeMux()}}
	app.Web.HandleFunc("POST /mutation", func(http.ResponseWriter, *http.Request) {
		t.Fatal("rejected request reached its handler")
	})
	logs := &capturedLogs{}
	handler, err := applicationHandler(app, newTestLogger(logs))
	if err != nil {
		t.Fatal(err)
	}
	body := &csrfUnreadBody{}
	r := httptest.NewRequest(http.MethodPost, "http://localhost/mutation", body)
	r.Header.Set("Origin", "https://attacker.invalid")
	r.Header.Set("HX-Request", "true")
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// Neither forwarded headers nor an invalid session cookie must bypass the check.
	r.Header.Set("X-Forwarded-Host", "attacker.invalid")
	r.Header.Set("X-Forwarded-Proto", "https")
	r.AddCookie(&http.Cookie{Name: "auth-user-session", Value: "invalid-token"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden || body.reads != 0 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("rejection touched request state: status=%d, body reads=%d, cookies=%v", w.Code, body.reads, w.Result().Cookies())
	}
	if w.Header().Get("X-Request-ID") == "" || !strings.Contains(logs.String(), "Request completed") {
		t.Fatal("rejected request was not logged")
	}
}

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

func TestSecurityHeadersPrecedeRejections(t *testing.T) {
	app := &application{ApplicationServer: &server.ApplicationServer{Config: &config.Config{}, Web: http.NewServeMux()}}
	configureTestProxy(app.Config)
	app.Config.HTTP.HSTSMaxAge = 86400
	app.Config.HTTP.SharedRateLimit = config.RateLimitConfig{Enabled: true, Requests: 2, Window: time.Minute, MaxClients: 10}
	handler, err := applicationHandler(app, newTestLogger(&capturedLogs{}))
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
	if handler, err := applicationHandler(app, newTestLogger(&capturedLogs{})); handler != nil || err == nil {
		t.Fatal("negative HSTS configuration accepted")
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
