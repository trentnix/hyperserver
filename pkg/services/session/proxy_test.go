package session

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/trentnix/hyperserver/pkg/requestinfo"
)

func TestSessionCookiesUseVerifiedHTTPS(t *testing.T) {
	for _, tc := range []struct {
		name, peer, scheme string
		tls, secure        bool
	}{
		{"proxy HTTPS", "192.0.2.1:1", "https", false, true},
		{"spoofed HTTPS", "192.0.2.2:1", "https", false, false},
		{"direct TLS", "192.0.2.2:1", "http", true, true},
		{"proxy HTTP over TLS", "192.0.2.1:1", "http", true, false},
	} {
		for _, provider := range []string{"cookie", "sqlite"} {
			for _, action := range []string{"save", "end", "rotate"} {
				if provider == "cookie" && action == "rotate" {
					continue
				}
				t.Run(tc.name+"/"+provider+"/"+action, func(t *testing.T) {
					proxy, err := requestinfo.NewProxyMiddleware([]string{"192.0.2.1/32"})
					if err != nil {
						t.Fatal(err)
					}
					var store SessionStore = setupCookieStore(t)
					if provider == "sqlite" {
						store = revocationStore(t)
					}
					r := httptest.NewRequest("POST", "http://example.test/", nil)
					r.RemoteAddr = tc.peer
					r.Header.Set("X-Forwarded-For", "203.0.113.1")
					r.Header.Set("X-Forwarded-Proto", tc.scheme)
					if tc.tls {
						r.TLS = &tls.ConnectionState{}
					}
					w := httptest.NewRecorder()
					proxy(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						s := newSession(store, "test-session")
						var err error
						switch action {
						case "save":
							err = store.Save(w, r, s)
						case "end":
							err = store.End(w, r, s)
						case "rotate":
							err = s.Rotate(w, r, map[string]any{"account": "test"})
						}
						if err != nil {
							t.Fatal(err)
						}
					})).ServeHTTP(w, r)
					cookies := w.Result().Cookies()
					if len(cookies) != 1 || cookies[0].Secure != tc.secure {
						t.Fatalf("cookies=%v, want Secure=%t", cookies, tc.secure)
					}
					if cookie := cookies[0]; !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.Domain != "" {
						t.Fatalf("unsafe cookie attributes: %s", cookie)
					}
					if action != "end" && cookies[0].MaxAge <= 0 {
						t.Fatal("saved cookie has no bounded browser lifetime")
					}
					if action == "end" && cookies[0].MaxAge != -1 {
						t.Fatal("logout did not expire the cookie")
					}
				})
			}
		}
	}
}
