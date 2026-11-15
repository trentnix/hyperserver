package blog

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

func installVideoFixture(t *testing.T, m *module) {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("video requests must not forward credentials")
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch r.URL.Path {
		case "/watch":
			switch r.URL.Query().Get("v") {
			case "ccccccccccc":
				http.NotFound(w, r)
			case "bbbbbbbbbbb":
				http.Redirect(w, r, "https://blocked.test/private", http.StatusFound)
			case "ddddddddddd":
				io.WriteString(w, `<head><title>Title only</title></head>`)
			case "eeeeeeeeeee":
				io.WriteString(w, `<head></head><body>No metadata</body>`)
			case "fffffffffff":
				io.WriteString(w, `<head><meta property="og:title" content="`+strings.Repeat("x", maxLinkHTML)+`"></head>`)
			case "ggggggggggg":
				http.Redirect(w, r, "https://www.youtube.com/watch?v=M7lc1UVf-VE", http.StatusFound)
			default:
				io.WriteString(w, `<head><meta property="og:title" content="Source video title"><meta property="og:description" content="Source video description."><meta property="og:image" content="https://blocked.test/unused.png"></head><body>`)
				io.WriteString(w, strings.Repeat("x", 2*maxLinkHTML))
			}
		case "/123456789/5e2d1c1e6d":
			io.WriteString(w, `<head><meta property="og:title" content="Source Vimeo title"><meta name="description" content="Source Vimeo description."></head>`)
		default:
			t.Errorf("unexpected video metadata request: %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := newLinkClient()
	transport := client.Transport.(*http.Transport)
	transport.TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig
	transport.TLSClientConfig.ServerName = "example.com"
	dialer := publicDialer{
		lookup: func(_ context.Context, _, host string) ([]netip.Addr, error) {
			if host == "blocked.test" {
				return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
			}
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		},
		dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			if address != "93.184.216.34:443" {
				t.Errorf("unexpected pinned video destination: %s", address)
			}
			return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(server.URL, "https://"))
		},
	}
	transport.DialContext = dialer.DialContext
	m.linkClient = client
}

func TestVideoFetchDetails(t *testing.T) {
	h := newHarness(t)
	installVideoFixture(t, h.blog)
	fields := url.Values{"url": {"https://youtu.be/M7lc1UVf-VE?t=90"}}
	requireStatus(t, h.request("POST", "/post/video-details", fields, false), http.StatusUnauthorized)
	h.register(t)
	h.verify(t)
	h.login(t)
	r := httptest.NewRequest("POST", "https://blog.example/post/video-details", strings.NewReader(fields.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://attacker.test")
	for _, cookie := range h.cookies {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.app.Handler.ServeHTTP(w, r)
	requireStatus(t, w, http.StatusForbidden)
	for _, tc := range []struct{ url, title, caption string }{
		{"https://youtu.be/M7lc1UVf-VE?t=90", "Source video title", "Source video description."},
		{"https://www.youtube-nocookie.com/embed/M7lc1UVf-VE", "Source video title", "Source video description."},
		{"https://player.vimeo.com/video/123456789?h=5e2d1c1e6d#t=45s", "Source Vimeo title", "Source Vimeo description."},
		{"https://youtu.be/ddddddddddd", "Title only", ""},
	} {
		fields.Set("url", tc.url)
		response := h.request("POST", "/post/video-details", fields, false)
		requireStatus(t, response, http.StatusOK)
		var result struct {
			Video   postVideo
			Warning string
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Video != (postVideo{URL: tc.url, Title: tc.title, Caption: tc.caption}) || (result.Warning != "") != (tc.caption == "") {
			t.Fatal("video details must preserve the URL, copy available text, and report a missing description", response.Body.String(), err)
		}
	}
	for _, raw := range []string{"https://attacker.test/video", "https://youtu.be/ccccccccccc", "https://youtu.be/bbbbbbbbbbb", "https://youtu.be/eeeeeeeeeee", "https://youtu.be/fffffffffff", "https://youtu.be/ggggggggggg"} {
		fields.Set("url", raw)
		requireStatus(t, h.request("POST", "/post/video-details", fields, false), http.StatusUnprocessableEntity)
	}
	// Video and link fetches share the same per-author budget.
	requireStatus(t, h.request("POST", "/post/link-preview", fields, false), http.StatusTooManyRequests)
}
