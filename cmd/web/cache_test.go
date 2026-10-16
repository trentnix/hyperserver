package main

import (
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func assertPageCacheHeaders(t *testing.T, header http.Header) {
	t.Helper()
	noStore := header.Get("Cache-Control") == "no-store"
	hasHXRequest := false
	hasHXHistory := false
	for _, value := range header.Values("Vary") {
		for _, field := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(field), "HX-Request") {
				hasHXRequest = true
			}
			if strings.EqualFold(strings.TrimSpace(field), "HX-History-Restore-Request") {
				hasHXHistory = true
			}
		}
	}
	if !noStore || !hasHXRequest || !hasHXHistory {
		t.Fatalf("unsafe rendered response headers: %v", header)
	}
}

func TestHTTPHistoryCachePolicy(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		for _, authenticated := range []bool{false, true} {
			if authenticated {
				h.establishSession(t, h.seedUser(t, "history@example.invalid"))
			}
			for _, hx := range []string{"", "true"} {
				r := httptest.NewRequest(http.MethodGet, h.baseURL+"/", nil)
				r.Header.Set("HX-Request", hx)
				r.Header.Set("HX-History-Restore-Request", "true")
				base, _ := url.Parse(h.baseURL)
				for _, cookie := range h.cookies.Cookies(base) {
					r.AddCookie(cookie)
				}
				w := httptest.NewRecorder()
				h.handler.ServeHTTP(w, r)
				assertPageCacheHeaders(t, w.Result().Header)
				body := w.Body.String()
				if w.Code != http.StatusOK || !strings.Contains(body, "<!DOCTYPE html>") || strings.Contains(body, "history@example.invalid") != authenticated {
					t.Fatal("history restore did not render the full page for its own account")
				}
			}
		}
	})
}

func TestHTTPCachePolicyAcrossUsersAndMutations(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		anonymous := h.cookies
		first := h.seedUser(t, "first@example.invalid")
		second := h.seedUser(t, "second@example.invalid")
		h.cookies, _ = cookiejar.New(nil)
		h.establishSession(t, first)
		firstCookies := h.cookies
		for _, htmx := range []bool{false, true} {
			w := h.request(http.MethodGet, "/", nil, htmx)
			assertPageCacheHeaders(t, w.Result().Header)
			if !strings.Contains(w.Body.String(), first.Email) {
				t.Fatal("first account was not rendered before mutation")
			}
		}
		h.cookies, _ = cookiejar.New(nil)
		h.establishSession(t, second)
		secondCookies := h.cookies

		first.Email = "updated@example.invalid"
		if err := h.app.AccountRepository.Update(t.Context(), first); err != nil {
			t.Fatal(err)
		}
		for _, client := range []struct {
			cookies http.CookieJar
			email   string
		}{{firstCookies, ""}, {secondCookies, second.Email}, {anonymous, ""}, {firstCookies, ""}} {
			h.cookies = client.cookies
			for _, htmx := range []bool{false, true} {
				w := h.request(http.MethodGet, "/", nil, htmx)
				if w.Code != http.StatusOK {
					t.Fatal(w.Code)
				}
				assertPageCacheHeaders(t, w.Result().Header)
				body := w.Body.String()
				for _, email := range []string{"first@example.invalid", first.Email, second.Email} {
					if strings.Contains(body, email) != (email == client.email) {
						t.Fatalf("response for %q has stale or cross-user identity %q", client.email, email)
					}
				}
			}
		}
		// Changing an email revokes the old session. A new login must show the new address.
		h.cookies = firstCookies
		h.establishSession(t, first)
		for _, htmx := range []bool{false, true} {
			w := h.request(http.MethodGet, "/", nil, htmx)
			assertPageCacheHeaders(t, w.Result().Header)
			if !strings.Contains(w.Body.String(), first.Email) || strings.Contains(w.Body.String(), "first@example.invalid") {
				t.Fatal("new login did not render the updated account")
			}
		}
	})
}

func TestHTTPPageAndFragmentCachePolicy(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		for _, authenticated := range []bool{false, true} {
			if authenticated {
				h.establishSession(t, h.seedUser(t, "cache@example.invalid"))
			}
			for _, htmx := range []bool{false, true} {
				w := h.request(http.MethodGet, "/", nil, htmx)
				if w.Code != http.StatusOK {
					t.Fatalf("home returned %d", w.Code)
				}
				assertPageCacheHeaders(t, w.Result().Header)
				if fullPage := strings.Contains(w.Body.String(), "<!DOCTYPE html>"); fullPage == htmx {
					t.Fatal("home did not select the expected page or fragment")
				}
				if personalized := strings.Contains(w.Body.String(), "cache@example.invalid"); personalized != authenticated {
					t.Fatal("response did not reflect its own account state")
				}
			}
		}
		for _, path := range []string{"/css/site.css", "/js/site.js"} {
			w := h.request(http.MethodGet, path, nil, false)
			if w.Code != http.StatusOK || w.Header().Get("Cache-Control") == "no-store" || strings.Contains(strings.Join(w.Header().Values("Vary"), ","), "HX-Request") {
				t.Fatalf("rendering policy affected static asset %s", path)
			}
		}
	})
}

func TestHTTPErrorCachePolicyPreservesVary(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		h.app.Web.HandleFunc("GET /test/cache-error", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("Vary", "Accept-Encoding, Origin")
			w.Header().Add("Vary", "Accept-Language")
			h.app.ContentManager.HandleError(w, r, "Cannot complete request", nil, http.StatusServiceUnavailable)
		})
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			for _, htmx := range []bool{false, true} {
				w := h.request(method, "/test/cache-error", nil, htmx)
				if w.Code != http.StatusServiceUnavailable {
					t.Fatalf("error response returned %d", w.Code)
				}
				assertPageCacheHeaders(t, w.Result().Header)
				vary := strings.Join(w.Result().Header.Values("Vary"), ",")
				for _, field := range []string{"Accept-Encoding", "Origin", "Accept-Language"} {
					if !strings.Contains(vary, field) {
						t.Fatalf("error renderer dropped Vary field %s", field)
					}
				}
				if method == http.MethodHead && w.Body.Len() != 0 {
					t.Fatal("HEAD error response included a body")
				}
			}
		}
	})
}
