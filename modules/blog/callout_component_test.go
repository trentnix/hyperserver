package blog

import (
	"encoding/xml"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
)

func TestCalloutValidation(t *testing.T) {
	for _, source := range []string{
		"null", `{"body":"Note","style":"warning"}`, `{"body":"Note","style":"amber-rail"}`, `{"body":"Note","style":"honey-fill"}`,
		componentJSON(postCallout{Title: "Heading only", Body: " \n\t"}),
		componentJSON(postCallout{Title: strings.Repeat("a", 241), Body: "Text"}),
		componentJSON(postCallout{Body: strings.Repeat("a", 20001)}),
	} {
		_, err := joinPostComponents("Post body", componentInput{"callout": source})
		if !slices.Contains(validationFields(err), "callout") {
			t.Fatalf("expected a callout validation error: %v", err)
		}
	}
}

func TestCalloutPostAndFeed(t *testing.T) {
	h := registeredBrowserHarness(t)
	h.login(t)
	image := mediaUpload(t, h.blog, "Callout image")
	note := postCallout{
		Title: "  Correction <Nov 20>  ",
		Body:  "Use the **updated** [guide](guide).\n\n![Diagram](/images/" + image.ID + ")\n\n<script>alert(1)</script>",
	}
	fields := url.Values{
		"title": {"Callout review"}, "slug": {"callout-review"}, "published": {"on"},
		"content": {"Before the correction.\n\n" + placementMarker("callout") + "\n\nAfter the correction."},
		"callout": {componentJSON(note)}, "componentOrder": {`["callout"]`},
	}
	requireStatus(t, h.request(http.MethodPost, "/post", fields, false), http.StatusSeeOther)
	post, err := h.blog.posts.GetBySlug(t.Context(), "callout-review", false)
	if err != nil {
		t.Fatal(err)
	}
	search := searchableWriting(post.Markdown)
	if !strings.Contains(search, "Correction <Nov 20>") || !strings.Contains(search, "updated") {
		t.Fatal("searchable writing omitted the callout heading or text")
	}
	clear(h.cookies)
	requireStatus(t, h.request(http.MethodGet, "/images/"+image.ID, nil, false), http.StatusOK)

	page := h.request(http.MethodGet, "/post/s/callout-review", nil, false)
	requireStatus(t, page, http.StatusOK)
	body := page.Body.String()
	before := strings.Index(body, "Before the correction.")
	position := strings.Index(body, `<div class="post-callout" role="note">`)
	if before < 0 || position <= before || position >= strings.Index(body, "After the correction.") ||
		!strings.Contains(body, "<strong>Correction &lt;Nov 20&gt;</strong>") ||
		!strings.Contains(body, "<strong>updated</strong>") || strings.Contains(body, "<script>alert(1)</script>") {
		t.Fatalf("unexpected callout markup: %s", body)
	}

	response := h.request(http.MethodGet, "/rss", nil, false)
	requireStatus(t, response, http.StatusOK)
	var rss feed
	if err := xml.Unmarshal(response.Body.Bytes(), &rss); err != nil || len(rss.Channel.Items) != 1 {
		t.Fatalf("expected one feed item: %v, %s", err, response.Body)
	}
	content := rss.Channel.Items[0].Content
	if !strings.Contains(content, "<strong>Correction &lt;Nov 20&gt;</strong>") ||
		!strings.Contains(content, `<strong>updated</strong> <a href="https://blog.example/post/s/guide">`) {
		t.Fatalf("feed omitted the callout or its resolved link: %s", content)
	}
}
