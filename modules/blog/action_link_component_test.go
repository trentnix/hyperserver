package blog

import (
	"encoding/xml"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestActionLinkValidation(t *testing.T) {
	for _, source := range []string{
		"", "null", `{"label":"View","url":"/page/projects","style":"dark"}`,
		componentJSON(postActionLink{Label: " ", URL: "/page/projects"}),
		componentJSON(postActionLink{Label: strings.Repeat("a", 241), URL: "/page/projects"}),
		componentJSON(postActionLink{Label: "View"}),
	} {
		_, err := joinPostComponents("Post body", componentInput{"actionLink": source, "componentOrder": `["action-link"]`})
		if !slices.Contains(validationFields(err), "actionLink") {
			t.Fatalf("expected an action link validation error for %q, got %v", source, err)
		}
	}
	for _, destination := range []string{
		"page/projects", "javascript:alert(1)", "//example.test", "/%2Fexample.test",
		`/\example.test`, "/%5Cexample.test", "/bad%2", "https://user@example.test/", "https://example.test:65536/",
		"/" + strings.Repeat("é", 400),
	} {
		if _, err := parseActionLink(componentJSON(postActionLink{Label: "View", URL: destination})); err == nil {
			t.Errorf("accepted invalid destination %q", destination)
		}
	}
}

func TestActionLinkPostAndFeed(t *testing.T) {
	h := registeredBrowserHarness(t)
	h.login(t)
	const extra = "action-link:11111111111111111111111111111111"
	fields := url.Values{
		"title": {"Action link review"}, "slug": {"action-link-review"}, "published": {"on"},
		"content":        {"Before the link.\n\n" + placementMarker("action-link") + "\n\nAfter the link."},
		"componentOrder": {componentJSON([]string{"action-link", extra})},
		"actionLink":     {componentJSON(postActionLink{Label: " View the projects <archive> ", URL: " /page/projects?tag=go&sort=new#examples "})},
		extra:            {componentJSON(postActionLink{Label: "Open the demo", URL: "https://example.test:8443/demo"})},
	}
	requireStatus(t, h.request(http.MethodPost, "/post", fields, false), http.StatusSeeOther)
	post, err := h.blog.posts.GetBySlug(t.Context(), "action-link-review", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(searchableWriting(post.Markdown), "View the projects <archive>") {
		t.Fatal("searchable writing omitted the action label")
	}
	clear(h.cookies)

	page := h.request(http.MethodGet, "/post/s/action-link-review", nil, false)
	requireStatus(t, page, http.StatusOK)
	body := page.Body.String()
	local := `<a class="button-primary" href="/page/projects?tag=go&amp;sort=new#examples"><span>View the projects &lt;archive&gt;</span></a>`
	external := `href="https://example.test:8443/demo" target="_blank" rel="noopener noreferrer" hx-boost="false" aria-label="Open the demo (opens in a new tab)"`
	placed := strings.Index(body, local)
	if placed < 0 || strings.Index(body, "Before the link.") >= placed || strings.Index(body, "After the link.") <= placed ||
		!strings.Contains(body, external) || strings.Count(body, `class="post-action-link"`) != 2 {
		t.Fatal("the page must render both links with the right destinations, tab behavior, and placement")
	}

	response := h.request(http.MethodGet, "/rss", nil, false)
	requireStatus(t, response, http.StatusOK)
	var rss feed
	if err := xml.Unmarshal(response.Body.Bytes(), &rss); err != nil || len(rss.Channel.Items) != 1 {
		t.Fatalf("expected one feed item: %v, %s", err, response.Body)
	}
	content := rss.Channel.Items[0].Content
	if !strings.Contains(content, `<a href="https://blog.example/page/projects?tag=go&amp;sort=new#examples">View the projects &lt;archive&gt;</a>`) ||
		!strings.Contains(content, `<a href="https://example.test:8443/demo">Open the demo</a>`) ||
		strings.Contains(content, `class="post-action-link"`) {
		t.Fatalf("expected plain links with absolute URLs in RSS: %s", content)
	}
}

func TestBrowserActionLink(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check action links")
	}
	testBrowser(t, browser, "320", browserScenario{
		scriptFile: "testdata/action-link.js", startPath: "/pages/create", authenticated: true,
		setup: func(t *testing.T) *harness {
			h := registeredBrowserHarness(t)
			h.login(t)
			return h
		},
	})
}
