package main

import (
	"net/http"
	"strings"
	"testing"
)

func assertPageCacheHeaders(t *testing.T, header http.Header) {
	t.Helper()
	noStore := header.Get("Cache-Control") == "no-store"
	hasHXRequest := false
	for _, value := range header.Values("Vary") {
		for _, field := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(field), "HX-Request") {
				hasHXRequest = true
			}
		}
	}
	if !noStore || !hasHXRequest {
		t.Fatalf("unsafe rendered response headers: %v", header)
	}
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
