package blog

import (
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const testEmbedURL = "https://widgets.example.test/demo?theme=light&scale=2"

func embedTestHarness(t *testing.T) *harness {
	t.Helper()
	cfg := harnessConfig(t, 10)
	path := filepath.Join(cfg.App.WorkingDirectory, "modules", "blog", "config.yaml")
	settings, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(settings, []byte("embedOrigins: [https://widgets.example.test]\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	h := startHarness(t, cfg)
	h.register(t)
	h.verify(t)
	if err := h.blog.access.setup.finish(t.Context()); err != nil {
		t.Fatal(err)
	}
	return h
}

func TestEmbedValidation(t *testing.T) {
	for _, input := range []string{"http://widgets.example.test", "https://*.example.test", "https://widgets.example.test:8443",
		"https://widgets.example.test/path", "https://widgets.example.test/?", "https://widgets.example.test/#", "https://user@widgets.example.test"} {
		if _, err := parseEmbedOrigins([]string{input}); err == nil {
			t.Errorf("accepted embed origin %q", input)
		}
	}
	origins, err := parseEmbedOrigins([]string{"https://WIDGETS.example.test:443/", "https://widgets.example.test"})
	if _, ok := origins["widgets.example.test"]; err != nil || len(origins) != 1 || !ok {
		t.Fatalf("expected a single normalized origin, got %v, %v", origins, err)
	}
	for _, tc := range []struct {
		url, title, field string
	}{
		{"http://widgets.example.test/demo", "Widget", "embed.url"},
		{"https://foo@example.test/demo", "Widget", "embed.url"},
		{"https://widgets.example.test:8080/demo", "Widget", "embed.url"},
		{testEmbedURL, " ", "embed.title"},
		{testEmbedURL, strings.Repeat("📊", 241), "embed.title"},
	} {
		if _, err := parseEmbed(componentJSON(postEmbed{URL: tc.url, Title: tc.title})); !slices.Equal(validationFields(err), []string{"embed", tc.field}) {
			t.Errorf("expected %s validation for %q: %v", tc.field, tc.url, err)
		}
	}
	if _, err := parseEmbed(`{"url":"https://widgets.example.test/demo","title":"Widget","html":"<iframe>"}`); err == nil {
		t.Fatal("accepted unsupported embed fields")
	}
}

func TestEmbedURLLength(t *testing.T) {
	const prefix = "https://widgets.example.test/"
	repeated := (maxEmbedURLBytes - len(prefix)) / len("%C3%A9")
	tail := strings.Repeat("a", maxEmbedURLBytes-len(prefix)-repeated*len("%C3%A9"))
	atLimit := prefix + strings.Repeat("é", repeated) + tail
	e, err := parseEmbed(componentJSON(postEmbed{URL: atLimit, Title: "Chart"}))
	if err != nil || len(e.URL) != maxEmbedURLBytes {
		t.Fatalf("expected a normalized URL at the limit: %v", err)
	}
	if saved, err := parseEmbed(e.EditorSource()); err != nil || e.URL != saved.URL {
		t.Fatalf("the accepted URL must survive a round trip: %v", err)
	}
	if _, err := parseEmbed(componentJSON(postEmbed{URL: atLimit + "a", Title: "Chart"})); !slices.Equal(validationFields(err), []string{"embed", "embed.url"}) {
		t.Fatalf("expected a URL error after normalization: %v", err)
	}
}

func TestEmbedDescription(t *testing.T) {
	e := postEmbed{URL: testEmbedURL, Title: "Chart", Description: strings.Repeat("é", maxEmbedDescription)}
	if saved, err := parseEmbed(e.EditorSource()); err != nil || saved.Description != e.Description || !strings.Contains(saved.SearchText(), e.Description) {
		t.Fatalf("the description at the limit must survive a round trip and be searchable: %v", err)
	}
	e.Description += "é"
	if _, err := parseEmbed(e.EditorSource()); !slices.Equal(validationFields(err), []string{"embed", "embed.description"}) {
		t.Fatalf("expected a description length error: %v", err)
	}
	e.Description = "  \n  "
	if saved, err := parseEmbed(e.EditorSource()); err != nil || saved.Description != "" {
		t.Fatalf("a whitespace-only description must be treated as empty: %v", err)
	}
}

func TestEmbedPreviewCheck(t *testing.T) {
	h := embedTestHarness(t)
	check := func(destination, title string, code int) map[string]any {
		t.Helper()
		query := url.Values{"url": {destination}, "title": {title}}
		req := httptest.NewRequest("GET", "https://blog.example"+embedFramePath+"?"+query.Encode(), nil)
		req.Header.Set("Accept", "application/json")
		w := httptest.NewRecorder()
		h.app.Handler.ServeHTTP(w, req)
		requireStatus(t, w, code)
		var result map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-src") {
			t.Fatal("the JSON response needs no frame permissions")
		}
		return result
	}
	for _, tc := range []struct {
		raw, normalized string
		allowed         bool
	}{
		{"https://WIDGETS.example.test:443/é", "https://widgets.example.test/%C3%A9", true},
		{"https://other.example.test/demo", "https://other.example.test/demo", false},
	} {
		if result := check(tc.raw, "Chart", http.StatusOK); result["url"] != tc.normalized || result["allowed"] != tc.allowed {
			t.Fatalf("unexpected preview check: %v", result)
		}
	}
	longURL := "https://widgets.example.test/" + strings.Repeat("é", 500)
	if result := check(longURL, "Chart", http.StatusBadRequest); result["field"] != "embed.url" {
		t.Fatalf("expected the preview to identify the URL field: %v", result)
	}
	if result := check(testEmbedURL, " ", http.StatusBadRequest); result["field"] != "embed.title" {
		t.Fatalf("expected the preview to identify the title field: %v", result)
	}
	h.login(t)
	form := url.Values{"title": {"Embed review"}, "published": {"on"},
		"embed": {componentJSON(postEmbed{URL: longURL, Title: "Chart"})}, "componentOrder": {`["embed"]`}}
	w := h.request("POST", "/post", form, true)
	requireStatus(t, w, http.StatusUnprocessableEntity)
	if !strings.Contains(w.Body.String(), `data-error-fields="embed embed.url"`) {
		t.Fatal("Save must identify the embed URL field")
	}
}

func TestEmbedFrameAndFallback(t *testing.T) {
	h := embedTestHarness(t)
	h.login(t)
	const title = "A <chart> & controls"
	const description = "Explore the **yearly changes** in the [chart notes](/chart-notes).\n\n<aside>Raw HTML</aside>"
	frameQuery := "?" + url.Values{"url": {testEmbedURL}, "title": {title}}.Encode()
	frame := h.request("GET", embedFramePath+frameQuery, nil, false)
	requireStatus(t, frame, http.StatusOK)
	policy := frame.Header().Get("Content-Security-Policy")
	if !strings.Contains(policy, "frame-src https://widgets.example.test;") || !strings.HasSuffix(policy, "frame-ancestors 'self'") ||
		frame.Header().Get("X-Frame-Options") != "SAMEORIGIN" || frame.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected frame headers: %v", frame.Header())
	}
	for _, expected := range []string{`src="https://widgets.example.test/demo?theme=light&amp;scale=2"`, `title="A &lt;chart&gt; &amp; controls"`, `sandbox="allow-scripts"`, `referrerpolicy="no-referrer"`} {
		if !strings.Contains(frame.Body.String(), expected) {
			t.Fatalf("missing %q in frame: %s", expected, frame.Body)
		}
	}
	other := "https://other.example.test/demo"
	blocked := h.request("GET", embedFramePath+"?"+url.Values{"url": {other}, "title": {"Widget"}}.Encode(), nil, false)
	requireStatus(t, blocked, http.StatusForbidden)
	if strings.Contains(blocked.Body.String(), "<iframe") || strings.Contains(blocked.Header().Get("Content-Security-Policy"), "frame-src") {
		t.Fatal("unapproved embed received frame permissions")
	}
	requireStatus(t, h.request("GET", embedFramePath+frameQuery+"&title=duplicate", nil, false), http.StatusBadRequest)

	for _, tc := range []struct {
		slug, destination string
		framed            bool
	}{{"approved-embed", testEmbedURL, true}, {"link-only-embed", other, false}} {
		form := url.Values{"title": {"Embed review"}, "slug": {tc.slug}, "published": {"on"},
			"embed": {componentJSON(postEmbed{URL: tc.destination, Title: title, Description: description})}, "componentOrder": {`["embed"]`}}
		requireStatus(t, h.request("POST", "/post", form, false), http.StatusSeeOther)
		page := h.request("GET", "/post/s/"+tc.slug, nil, false)
		requireStatus(t, page, http.StatusOK)
		if strings.Contains(page.Body.String(), `class="web-embed-frame"`) != tc.framed ||
			!strings.Contains(page.Body.String(), "Open A &lt;chart&gt; &amp; controls") ||
			strings.Contains(page.Header().Get("Content-Security-Policy"), "https://widgets.example.test") {
			t.Fatalf("unexpected reader output for %s: %s", tc.slug, page.Body)
		}
		body := page.Body.String()
		descriptionIndex := strings.Index(body, `<div class="web-embed-description"><p>Explore the <strong>yearly changes</strong> in the <a href="/chart-notes">chart notes</a>`)
		next := `Open A &lt;chart&gt;`
		if tc.framed {
			next = `class="web-embed-frame"`
		}
		if descriptionIndex < 0 || descriptionIndex >= strings.Index(body, next) || strings.Contains(body, "<aside>Raw HTML</aside>") {
			t.Fatalf("expected the Markdown description before the embed or link on %s", tc.slug)
		}
	}
	w := h.request("GET", "/rss", nil, false)
	requireStatus(t, w, http.StatusOK)
	var rss feed
	if err := xml.Unmarshal(w.Body.Bytes(), &rss); err != nil {
		t.Fatal(err)
	}
	if len(rss.Channel.Items) != 2 {
		t.Fatalf("expected two feed items, got %d", len(rss.Channel.Items))
	}
	for _, item := range rss.Channel.Items {
		if strings.Contains(item.Content, "web-embed-frame") || !strings.Contains(item.Content, `Open A &lt;chart&gt;`) ||
			!strings.Contains(item.Content, `<strong>yearly changes</strong>`) || !strings.Contains(item.Content, `/chart-notes">chart notes</a>`) {
			t.Fatalf("feed must include the description and link: %s", item.Content)
		}
	}
}
