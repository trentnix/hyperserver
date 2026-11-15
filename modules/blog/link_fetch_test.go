package blog

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLinkDestinations(t *testing.T) {
	for _, raw := range []string{"file:///etc/passwd", "javascript:alert(1)", "//example.com", "https://user:secret@example.com/", "https://example.com:8443/", "http://example.com:/", "http://[fe80::1%25eth0]/", "https://", "https://example.com/" + strings.Repeat("a", 2048)} {
		if _, err := linkURL(raw); err == nil {
			t.Errorf("accepted invalid URL %q", raw)
		}
	}
	for _, ip := range []string{"0.1.2.3", "10.0.0.1", "100.100.100.200", "127.0.0.1", "169.254.169.254", "172.16.0.1", "192.168.1.1", "192.0.0.1", "192.0.2.1", "198.18.0.1", "198.51.100.1", "203.0.113.1", "224.0.0.1", "240.0.0.1", "::", "::1", "::ffff:127.0.0.1", "fc00::1", "fe80::1", "64:ff9b::7f00:1", "2001:db8::1", "2002:7f00:1::", "3fff::1"} {
		if publicPreviewAddress(netip.MustParseAddr(ip)) {
			t.Errorf("accepted nonpublic address %s", ip)
		}
	}
	for _, ip := range []string{"93.184.216.34", "8.8.8.8", "::ffff:8.8.8.8", "2606:4700:4700::1111"} {
		if !publicPreviewAddress(netip.MustParseAddr(ip)) {
			t.Errorf("rejected public address %s", ip)
		}
	}
	for _, ips := range [][]netip.Addr{{netip.MustParseAddr("127.0.0.1")}, {netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("::1")}, nil} {
		dialer := publicDialer{
			lookup: func(context.Context, string, string) ([]netip.Addr, error) { return ips, nil },
			dial: func(context.Context, string, string) (net.Conn, error) {
				t.Fatal("blocked destination reached the dialer")
				return nil, nil
			},
		}
		if _, err := dialer.DialContext(t.Context(), "tcp", "example.com:80"); err == nil {
			t.Fatal("unsafe DNS result accepted", ips)
		}
	}
	lookups := 0
	dialer := publicDialer{
		lookup: func(context.Context, string, string) ([]netip.Addr, error) {
			lookups++
			return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
		},
		dial: func(_ context.Context, _, address string) (net.Conn, error) {
			if address != "8.8.8.8:443" {
				t.Fatal("dial did not pin the validated address", address)
			}
			return nil, errors.New("test dial")
		},
	}
	dialer.DialContext(t.Context(), "tcp", "example.com:443")
	if lookups != 1 {
		t.Fatal("connection performed another DNS lookup")
	}
}

// Requests keep real public-looking URLs and the production transport's address
// checks. Only the final socket destination changes to this isolated test server.
func linkTestClient(t *testing.T, handler http.Handler) *http.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := newLinkClient()
	transport := client.Transport.(*http.Transport)
	dialer := publicDialer{
		lookup: func(_ context.Context, _, host string) ([]netip.Addr, error) {
			if host == "blocked.test" {
				return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
			}
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		},
		dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			if address != "93.184.216.34:80" {
				t.Errorf("unexpected pinned destination %s", address)
			}
			return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(server.URL, "http://"))
		},
	}
	transport.DialContext = dialer.DialContext
	return client
}

func TestLinkFetchTransport(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	client := linkTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("credentials forwarded")
		}
		switch r.URL.Path {
		case "/blocked":
			http.Redirect(w, r, "http://blocked.test/private", http.StatusFound)
		case "/redirect":
			http.Redirect(w, r, "/redirect", http.StatusFound)
		case "/large":
			io.WriteString(w, strings.Repeat("x", maxLinkHTML+1))
		case "/compressed":
			w.Header().Set("Content-Encoding", "gzip")
			out := gzip.NewWriter(w)
			out.Write(bytes.Repeat([]byte("x"), maxLinkHTML+1))
			out.Close()
		case "/slow":
			<-r.Context().Done()
		default:
			w.Header().Set("Content-Type", "text/html")
			io.WriteString(w, "<title>Page</title>")
		}
	}))
	if transport := client.Transport.(*http.Transport); transport.Proxy != nil || client.Jar != nil {
		t.Fatal("fetcher must not use proxies or cookies")
	}
	if _, _, _, err := fetchLinkBytes(t.Context(), client, "http://example.test/ok", "text/html", maxLinkHTML); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"blocked", "redirect", "large", "compressed"} {
		if _, _, _, err := fetchLinkBytes(t.Context(), client, "http://example.test/"+path, "text/html", maxLinkHTML); err == nil {
			t.Fatal("accepted unsafe response", path)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if _, _, _, err := fetchLinkBytes(ctx, client, "http://example.test/slow", "text/html", maxLinkHTML); err == nil {
		t.Fatal("ignored request timeout")
	}
	request, _ := http.NewRequest("GET", "http://example.test/", nil)
	previous, _ := http.NewRequest("GET", "https://example.test/", nil)
	if client.CheckRedirect(request, []*http.Request{previous}) == nil {
		t.Fatal("allowed HTTPS downgrade")
	}
	original, _ := http.NewRequest("GET", "http://example.test/", nil)
	if client.CheckRedirect(request, []*http.Request{original, previous}) == nil {
		t.Fatal("allowed a downgrade after upgrading to HTTPS")
	}
}

func TestLinkMetadata(t *testing.T) {
	u, _ := url.Parse("https://example.test/article")
	link, image := linkMetadata([]byte(`<head><title>Fallback &amp; title</title><meta name="description" content="Fallback description"><meta property="og:title" content="Chosen &lt;title&gt;"><meta property="og:description" content="  Chosen description  "><meta property="og:site_name" content="A site"><meta property="og:image" content="../picture.png"><meta property="og:url" content="https://attacker.test/"></head><body><meta property="og:title" content="Ignored"></body>`), u)
	if link.URL != u.String() || link.Title != "Chosen <title>" || link.Description != "Chosen description" || link.SiteName != "A site" || image != "../picture.png" {
		t.Fatal("incorrect metadata", link, image)
	}
	link, _ = linkMetadata([]byte(`<title>Fallback &amp; title</title><meta name="description" content="Fallback description">`), u)
	if link.Title != "Fallback & title" || link.Description != "Fallback description" {
		t.Fatal("missing HTML fallback", link)
	}
	if got := metadataText(strings.Repeat("界", 300), 240); len([]rune(got)) != 240 {
		t.Fatal("metadata limits must count characters")
	}
}

func TestLinkFetchTextEncoding(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	h.blog.linkClient = linkTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/legacy" {
			w.Header().Set("Content-Type", "text/html; charset=windows-1252")
			io.WriteString(w, "<title>Caf\xe9</title>")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, `<title>Café 日本語</title><meta name="description" content="Résumé &amp; notes">`)
	}))
	fields := url.Values{"url": {"http://example.test/legacy"}}
	response := h.request("POST", "/post/link-preview", fields, false)
	requireStatus(t, response, http.StatusUnprocessableEntity)
	var failure map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &failure); err != nil || !strings.Contains(failure["error"], "unsupported text encoding") {
		t.Fatal("legacy encodings must offer manual entry instead of corrupted metadata", response.Body.String(), err)
	}
	fields.Set("url", "http://example.test/utf8")
	response = h.request("POST", "/post/link-preview", fields, false)
	requireStatus(t, response, http.StatusOK)
	var result struct{ Link postLink }
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Link.Title != "Café 日本語" || result.Link.Description != "Résumé & notes" {
		t.Fatal("UTF-8 metadata must preserve accented and non-Latin text", response.Body.String(), err)
	}
}

func TestLinkFetchBudget(t *testing.T) {
	var budget linkFetchBudget
	now := time.Now()
	var group sync.WaitGroup
	for range 20 {
		group.Go(func() { budget.allow("same-author", now) })
	}
	group.Wait()
	if budget.windows["same-author"].count != 10 || budget.allow("same-author", now) || !budget.allow("another-author", now) || !budget.allow("same-author", now.Add(time.Minute)) {
		t.Fatal("per-author budget did not expire independently")
	}
	budget.windows = make(map[string]linkFetchWindow)
	for i := range 1024 {
		budget.windows[strconv.Itoa(i)] = linkFetchWindow{until: now.Add(time.Minute)}
	}
	if budget.allow("extra", now) || len(budget.windows) != 1024 {
		t.Fatal("budget did not enforce its account bound")
	}
}

func sampleLinkImage(t *testing.T) []byte {
	t.Helper()
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 12, 8))); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func installLinkFixture(t *testing.T, m *module) {
	t.Helper()
	image := sampleLinkImage(t)
	m.linkClient = linkTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/article":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			io.WriteString(w, `<title>Fallback</title><meta property="og:title" content="An interesting article"><meta property="og:description" content="A useful article description."><meta property="og:site_name" content="Example publication"><meta property="og:image" content="/image.png">`)
		case "/image.png":
			w.Header().Set("Content-Type", "image/png")
			w.Write(image)
		case "/bad-image":
			w.Header().Set("Content-Type", "text/html")
			io.WriteString(w, `<title>Still usable</title><meta property="og:image" content="http://blocked.test/private.png">`)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestLinkFetchEndpoint(t *testing.T) {
	h := newHarness(t)
	installLinkFixture(t, h.blog)
	fields := url.Values{"url": {"http://example.test/article"}}
	requireStatus(t, h.request("POST", "/post/link-preview", fields, false), http.StatusUnauthorized)
	h.register(t)
	h.verify(t)
	h.login(t)
	r := httptest.NewRequest("POST", "https://blog.example/post/link-preview", strings.NewReader(fields.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://attacker.test")
	for _, cookie := range h.cookies {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.app.Handler.ServeHTTP(w, r)
	requireStatus(t, w, http.StatusForbidden)
	w = h.request("POST", "/post/link-preview", fields, false)
	requireStatus(t, w, http.StatusOK)
	var result struct {
		Link    postLink
		Warning string
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Link.Title != "An interesting article" || !validImageID(result.Link.ImageID) || result.Warning != "" {
		t.Fatal("missing imported preview", w.Body.String())
	}
	metadata, err := h.blog.mediaMetadata.GetMediaMetadata(t.Context(), result.Link.ImageID)
	account, _ := h.app.Accounts.GetByEmail(t.Context(), "trentnix@gmail.com")
	if err != nil || metadata.UploadedBy != account.ID {
		t.Fatal("thumbnail must belong to its importer", metadata, err)
	}
	fields.Set("url", "http://example.test/bad-image")
	w = h.request("POST", "/post/link-preview", fields, false)
	requireStatus(t, w, http.StatusOK)
	result = struct {
		Link    postLink
		Warning string
	}{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Warning == "" || result.Link.ImageID != "" {
		t.Fatal("blocked image must leave a usable card and warning", w.Body.String())
	}
	fields.Set("url", "http://example.test/missing")
	requireStatus(t, h.request("POST", "/post/link-preview", fields, false), http.StatusUnprocessableEntity)
	for range 7 {
		h.request("POST", "/post/link-preview", fields, false)
	}
	requireStatus(t, h.request("POST", "/post/link-preview", fields, false), http.StatusTooManyRequests)
}

func TestLinkPosterImports(t *testing.T) {
	h := usersHarness(t)
	installLinkFixture(t, h.blog)
	fields := url.Values{"url": {"http://example.test/article"}}
	response := h.request("POST", "/post/link-preview", fields, false)
	requireStatus(t, response, http.StatusOK)
	var result struct {
		Link    postLink
		Warning string
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	privateImage := result.Link.ImageID
	registeredPoster(t, h)
	response = h.request("POST", "/post/link-preview", fields, false)
	requireStatus(t, response, http.StatusOK)
	result = struct {
		Link    postLink
		Warning string
	}{}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Warning == "" || result.Link.ImageID != "" {
		t.Fatal("import exposed another author's private thumbnail", response.Body.String())
	}
	forged := postLink{URL: "https://example.test/article", Title: "Foreign image", ImageID: privateImage, HideImage: true}
	rejected := h.request("POST", "/post", url.Values{"title": {"Foreign link"}, "link": {linkSource(t, forged)}}, false)
	requireStatus(t, rejected, http.StatusUnprocessableEntity)
	if !strings.Contains(rejected.Body.String(), "Choose media you uploaded") {
		t.Fatal("foreign thumbnail must fail the shared media ownership check")
	}
}
