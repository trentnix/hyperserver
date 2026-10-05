package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/requestinfo"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/util"
)

func configureTestProxy(cfg *config.Config) {
	cfg.HTTP.TrustedProxies = []string{"192.0.2.1/32"}
	cfg.HTTP.PublicOrigin = "https://example.test"
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

func TestHTTPProxyRateLimits(t *testing.T) {
	for _, budget := range []string{"route", "shared"} {
		t.Run(budget, func(t *testing.T) {
			runHTTPScenario(t, func(h *httpHarness) {
				for _, tc := range []struct {
					client    string
					untrusted bool
					want      int
				}{
					{"203.0.113.1", false, 200},
					{"203.0.113.1", false, 429},
					{"203.0.113.2", false, 200},
					{"203.0.113.2", false, 429},
					{"203.0.113.3", true, 200},
					{"203.0.113.4", true, 429},
				} {
					r := proxyRequest(http.MethodGet, "/contact", tc.client, nil)
					if tc.untrusted {
						r.RemoteAddr = "192.0.2.2:1234"
					}
					w := httptest.NewRecorder()
					h.handler.ServeHTTP(w, r)
					if w.Code != tc.want {
						t.Fatalf("client=%s untrusted=%t status=%d, want %d", tc.client, tc.untrusted, w.Code, tc.want)
					}
				}
			}, configureTestProxy, func(cfg *config.Config) {
				limit := config.RateLimitConfig{Enabled: true, Requests: 1, Window: time.Minute, MaxClients: 10}
				if budget == "shared" {
					cfg.HTTP.SharedRateLimit = limit
				} else {
					cfg.HTTP.DefaultRateLimit = limit
				}
			})
		})
	}
}

func TestInvalidProxyMetadataPrecedesApplicationWork(t *testing.T) {
	app := &server.ApplicationServer{Config: &config.Config{}, Web: http.NewServeMux()}
	configureTestProxy(app.Config)
	app.Config.HTTP.SharedRateLimit = config.RateLimitConfig{Enabled: true, Requests: 1, Window: time.Minute, MaxClients: 10}
	handler, err := applicationHandler(app, &testLogger{logs: &capturedLogs{}})
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
	if handler, err := applicationHandler(app, &testLogger{logs: &capturedLogs{}}); err == nil || handler != nil {
		t.Fatal("invalid proxy configuration accepted")
	}
}

func TestHTTPThroughTLSProxy(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		h.app.Web.HandleFunc("GET /test-proxy", func(w http.ResponseWriter, r *http.Request) {
			client, err := requestinfo.ClientIP(r)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			link, err := util.BuildPublicURL(r, h.app.Config.HTTP, "/auth/verify", nil)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			s, err := session.New(r, "proxy-test")
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			if err := s.Save(w, r); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			fmt.Fprintf(w, "%s %t %t %s %s", client, requestinfo.IsHTTPS(r), r.TLS != nil, r.Host, link)
		})
		backend := httptest.NewServer(h.handler)
		defer backend.Close()
		target, err := url.Parse(backend.URL)
		if err != nil {
			t.Fatal(err)
		}
		transport := &http.Transport{}
		defer transport.CloseIdleConnections()
		proxy := httptest.NewTLSServer(&httputil.ReverseProxy{
			Transport: transport,
			Rewrite: func(r *httputil.ProxyRequest) {
				r.SetURL(target)
				r.Out.Host = r.In.Host
				r.SetXForwarded()
			},
		})
		defer proxy.Close()
		client := proxy.Client()
		client.Timeout = 5 * time.Second
		defer client.CloseIdleConnections()

		for i, spoofedIP := range []string{"203.0.113.1", "203.0.113.2"} {
			r, err := http.NewRequest(http.MethodGet, proxy.URL+"/test-proxy", nil)
			if err != nil {
				t.Fatal(err)
			}
			r.Host = "example.test"
			r.Header.Set("X-Forwarded-For", spoofedIP)
			r.Header.Set("X-Forwarded-Proto", "http")
			r.Header.Set("X-Forwarded-Host", "attacker.test")
			response, err := client.Do(r)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(response.Body)
			response.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if i == 0 {
				want := "127.0.0.1 true false example.test https://example.test/auth/verify"
				if response.StatusCode != 200 || string(body) != want {
					t.Fatalf("proxy response: status=%d body=%q", response.StatusCode, body)
				}
				cookies := response.Cookies()
				if len(cookies) != 1 || cookies[0].Name != "proxy-test" || !cookies[0].Secure {
					t.Fatalf("proxy response cookies=%v", cookies)
				}
			} else if response.StatusCode != 429 {
				t.Fatalf("spoofed client bypassed shared limit: status=%d", response.StatusCode)
			}
		}
	}, func(cfg *config.Config) {
		cfg.HTTP.TrustedProxies = []string{"127.0.0.1/32"}
		cfg.HTTP.PublicOrigin = "https://example.test"
		cfg.HTTP.SharedRateLimit = config.RateLimitConfig{Enabled: true, Requests: 1, Window: time.Minute, MaxClients: 10}
	})
}

func TestHTTPProxyLoginLogoutAndOriginProtection(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		u := h.seedUser(t, "proxy@example.invalid")
		form := url.Values{"email": {u.Email}, "password": {"TestPassword1!"}}
		for _, origin := range []string{"https://attacker.test", "https://example.test"} {
			r := proxyRequest(http.MethodPost, "/auth/login/email", "203.0.113.1", form)
			r.Header.Set("Origin", origin)
			r.Header.Set("X-Forwarded-Host", "attacker.test")
			w := httptest.NewRecorder()
			h.handler.ServeHTTP(w, r)
			if origin != "https://example.test" {
				if w.Code != 403 || len(w.Result().Cookies()) != 0 {
					t.Fatalf("cross-origin login: status=%d cookies=%v", w.Code, w.Result().Cookies())
				}
				continue
			}
			if w.Code != http.StatusSeeOther {
				t.Fatalf("login: status=%d body=%s", w.Code, w.Body.String())
			}
			var authCookie *http.Cookie
			for _, cookie := range w.Result().Cookies() {
				if !cookie.Secure {
					t.Errorf("login cookie %s is not Secure", cookie.Name)
				}
				if cookie.Name == "auth-user-session" {
					authCookie = cookie
				}
			}
			if authCookie == nil {
				t.Fatal("login did not issue authentication cookie")
			}

			r = proxyRequest(http.MethodPost, "/auth/logout", "203.0.113.1", nil)
			r.AddCookie(authCookie)
			w = httptest.NewRecorder()
			h.handler.ServeHTTP(w, r)
			if w.Code != http.StatusSeeOther {
				t.Fatalf("logout: status=%d", w.Code)
			}
			for _, cookie := range w.Result().Cookies() {
				if cookie.Name == "auth-user-session" && cookie.Secure && cookie.MaxAge < 0 {
					return
				}
			}
			t.Fatal("logout did not expire the Secure authentication cookie")
		}
	}, configureTestProxy)
}
