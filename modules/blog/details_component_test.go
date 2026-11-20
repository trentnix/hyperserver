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

func TestExpandableDetailsValidation(t *testing.T) {
	for _, source := range []string{
		"null", `{"title":"One","body":"Two","open":true}`,
		componentJSON(postDetails{Title: " ", Body: "Details"}),
		componentJSON(postDetails{Title: "Title", Body: " \n\t"}),
		componentJSON(postDetails{Title: strings.Repeat("a", 241), Body: "Details"}),
		componentJSON(postDetails{Title: "Title", Body: strings.Repeat("a", 20001)}),
	} {
		_, err := joinPostComponents("Post body", componentInput{"details": source})
		if !slices.Contains(validationFields(err), "details") {
			t.Fatalf("expected a details validation error: %v", err)
		}
	}
}

func TestExpandableDetailsPostAndFeed(t *testing.T) {
	h := registeredBrowserHarness(t)
	h.login(t)
	image := mediaUpload(t, h.blog, "Details image")
	details := postDetails{
		Title: "  Measurements <baseline>  ",
		Body:  "**Five** trials. [See notes](notes).\n\n![Baseline](/images/" + image.ID + ")\n\n<script>alert(1)</script>",
	}
	fields := url.Values{
		"title": {"Details review"}, "slug": {"details-review"}, "published": {"on"},
		"content": {"Before details.\n\n" + placementMarker("expandable-details") + "\n\nAfter details."},
		"details": {componentJSON(details)}, "componentOrder": {`["expandable-details"]`},
	}
	requireStatus(t, h.request(http.MethodPost, "/post", fields, false), http.StatusSeeOther)
	post, err := h.blog.posts.GetBySlug(t.Context(), "details-review", false)
	if err != nil {
		t.Fatal(err)
	}
	_, components, err := splitPostComponents(post.Markdown)
	details.Title = strings.TrimSpace(details.Title)
	if err != nil || len(components.Components) != 1 || components.Components[0].Content.EditorSource() != componentJSON(details) {
		t.Fatalf("saved details changed: %v, %v", components, err)
	}
	if searchable := searchableWriting(post.Markdown); !strings.Contains(searchable, details.Title) || !strings.Contains(searchable, "trials") {
		t.Fatal("searchable writing omitted the title or body")
	}
	refs, err := contentMediaReferences(post.Markdown, "/post/s/details-review", "")
	if err != nil || !slices.Contains(refs, "/images/"+image.ID) {
		t.Fatalf("details image reference missing: %v, %v", refs, err)
	}

	page := h.request(http.MethodGet, "/post/s/details-review", nil, false)
	requireStatus(t, page, http.StatusOK)
	body := page.Body.String()
	collapsed := strings.Index(body, `<details class="expandable-details"><summary>Measurements &lt;baseline&gt;</summary>`)
	if collapsed < 0 || strings.Index(body, "Before details.") >= collapsed || strings.Index(body, "After details.") <= collapsed ||
		!strings.Contains(body, "<strong>Five</strong> trials.") || strings.Contains(body, "<script>alert(1)</script>") {
		t.Fatalf("unexpected reader details markup: %s", body)
	}

	response := h.request(http.MethodGet, "/rss", nil, false)
	requireStatus(t, response, http.StatusOK)
	var rss feed
	if err := xml.Unmarshal(response.Body.Bytes(), &rss); err != nil || len(rss.Channel.Items) != 1 {
		t.Fatalf("expected one feed item: %v, %s", err, response.Body)
	}
	content := rss.Channel.Items[0].Content
	if strings.Contains(content, "<details") || !strings.Contains(content, "<strong>Measurements &lt;baseline&gt;</strong>") ||
		!strings.Contains(content, "<strong>Five</strong> trials.") || !strings.Contains(content, `href="https://blog.example/post/s/notes"`) {
		t.Fatalf("feed did not include expanded details with absolute links: %s", content)
	}
}

func TestBrowserExpandableDetails(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check expandable details")
	}
	for _, width := range []string{"320", "1100"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/details.js", startPath: "/post/create", authenticated: true,
				setup: func(t *testing.T) *harness {
					h := registeredBrowserHarness(t)
					h.login(t)
					return h
				},
			})
		})
	}
}
