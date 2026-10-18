package web_test

import (
	"crypto/sha512"
	"encoding/base64"
	"mime"
	"net/http"
	"net/http/httptest"
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

func TestHTTPVendoredHTMX(t *testing.T) {
	runSiteScenario(t, func(h *httpHarness) {
		const path = "/js/vendor/htmx/2.0.11/htmx.min.js"
		const integrity = "sha384-2OatzQy1H+Zd/IIrjr1TcuDGqLXeHhbooAyJY1KdQMKnr4LZ22k31GBLdYKHmVjg"
		page := h.request(http.MethodGet, "/", nil, false)
		if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `src="`+path+`"`) || !strings.Contains(page.Body.String(), `integrity="`+integrity+`"`) {
			t.Fatal("page does not pin the local HTMX asset and expected integrity hash")
		}

		for _, hx := range []bool{false, true} {
			asset := h.request(http.MethodGet, path, nil, hx)
			mediaType, _, err := mime.ParseMediaType(asset.Header().Get("Content-Type"))
			if asset.Code != http.StatusOK || err != nil || mediaType != "text/javascript" && mediaType != "application/javascript" {
				t.Fatalf("HTMX asset: status=%d, headers=%v", asset.Code, asset.Header())
			}
			digest := sha512.Sum384(asset.Body.Bytes())
			if "sha384-"+base64.StdEncoding.EncodeToString(digest[:]) != integrity {
				t.Fatal("served HTMX bytes do not match the pinned upstream build")
			}
			if asset.Header().Get("Cache-Control") == "no-store" || len(asset.Result().Cookies()) != 0 {
				t.Fatal("static HTMX asset acquired a page cache policy or session cookie")
			}
			head := h.request(http.MethodHead, path, nil, hx)
			if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Length") != asset.Header().Get("Content-Length") {
				t.Fatalf("HTMX HEAD: status=%d, headers=%v", head.Code, head.Header())
			}
		}
	})
}
