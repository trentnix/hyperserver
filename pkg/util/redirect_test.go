package util

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSafeRedirectURL(t *testing.T) {
	for _, destination := range []string{
		"/", "/account", "/account?tab=settings&next=%2Fhome#details",
		"/hello%20world", "/search?q=https%3A%2F%2Fexample.com", "/page#section",
	} {
		if got := SafeRedirectURL(destination, "/home"); got != destination {
			t.Errorf("SafeRedirectURL(%q) = %q", destination, got)
		}
	}
	for _, destination := range []string{
		"", "account", "?next=/", "#section", "https://example.com/home",
		"http://localhost/home", "javascript:alert(1)", "data:text/html,test",
		"//attacker.invalid", "///attacker.invalid", "//?query", "//#fragment",
		`/\attacker.invalid`, `\attacker.invalid`, "/%5cattacker.invalid", "/%2fattacker.invalid",
		"/%2F%2fattacker.invalid", " /home", "/bad%", "/page?q=%zz", "/page#%zz",
		"/page\r\nX-Injected: yes", "/page\t", "/page\x00", "/page\x7f",
		"/%0d%0aX-Injected:yes", "/page?q=%0a", "/page#%09", "/page?q=%5c",
	} {
		if got := SafeRedirectURL(destination, "/home?from=login"); got != "/home?from=login" {
			t.Errorf("unsafe destination %q returned %q", destination, got)
		}
		if got := SafeRedirectURL(destination, destination); got != "/" {
			t.Errorf("unsafe fallback %q returned %q", destination, got)
		}
	}
}

func TestRedirectToURL(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
		for _, htmx := range []bool{false, true} {
			for _, tc := range []struct{ destination, want string }{
				{"/account?tab=settings#details", "/account?tab=settings#details"},
				{"//attacker.invalid", "/"},
				{"https://example.com/same-host", "/"},
				{"/page\r\nX-Injected: yes", "/"},
				{`/search?q="><script>alert(1)</script>`, `/search?q="><script>alert(1)</script>`},
			} {
				r := httptest.NewRequest(method, "https://example.com/start", nil)
				r.Header.Set("X-Forwarded-Host", "attacker.invalid")
				if htmx {
					r.Header.Set("HX-Request", "true")
				}
				w := httptest.NewRecorder()
				RedirectToURL(w, r, tc.destination)
				status, header, absent := http.StatusSeeOther, "Location", "HX-Redirect"
				if htmx {
					status, header, absent = http.StatusOK, "HX-Redirect", "Location"
				}
				if w.Code != status || w.Header().Get(header) != tc.want || w.Header().Get(absent) != "" {
					t.Fatalf("%s htmx=%t destination=%q: status=%d headers=%v", method, htmx, tc.destination, w.Code, w.Header())
				}
				if w.Body.Len() != 0 || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Injected") != "" {
					t.Fatalf("unsafe redirect response: %v %s", w.Header(), w.Body.String())
				}
			}
		}
	}
}

func TestRedirectPostBecomesGet(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /start", func(w http.ResponseWriter, r *http.Request) {
		RedirectToURL(w, r, "/destination?tab=settings")
	})
	mux.HandleFunc("GET /destination", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "tab=settings" || r.ContentLength != 0 {
			t.Error("redirect lost the query or repeated the POST body")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	s := httptest.NewServer(mux)
	defer s.Close()
	response, err := s.Client().Post(s.URL+"/start", "application/x-www-form-urlencoded", strings.NewReader("secret=value"))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent || response.Request.Method != http.MethodGet {
		t.Fatalf("redirect did not navigate with GET: %d %s", response.StatusCode, response.Request.Method)
	}
}

func FuzzSafeRedirectURL(f *testing.F) {
	for _, seed := range []string{"/", "/home?x=1#fragment", "//attacker.invalid", "/%2fexample.com", "/%0a", "javascript:alert(1)"} {
		f.Add(seed, seed)
	}
	f.Fuzz(func(t *testing.T, destination, fallback string) {
		got := SafeRedirectURL(destination, fallback)
		u, err := url.Parse(got)
		if err != nil || !strings.HasPrefix(got, "/") || strings.HasPrefix(got, "//") || u.IsAbs() || u.Host != "" {
			t.Fatalf("non-local redirect %q", got)
		}
		decoded, err := url.PathUnescape(got)
		if err != nil || strings.ContainsFunc(decoded, func(c rune) bool { return c < 0x20 || c == 0x7f || c == '\\' }) {
			t.Fatalf("unsafe redirect characters in %q", got)
		}
	})
}
