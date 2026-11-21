package blog

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestTableOfContentsRendering(t *testing.T) {
	h := registeredBrowserHarness(t)
	h.login(t)
	body := "## Route &amp; timing\n\n" + placementMarker("table-of-contents") + "\n\n" +
		"### Bring **water**\n\n## Route &amp; timing\n\n### Café [hours](/about)\n\n" +
		"#### Extra detail\n\n> ## Quoted heading"
	fields := url.Values{
		"title": {"TOC review"}, "slug": {"toc-review"}, "published": {"on"}, "content": {body},
		"tableOfContents": {"{}"}, "componentOrder": {`["table-of-contents"]`},
	}
	requireStatus(t, h.request(http.MethodPost, "/post", fields, false), http.StatusSeeOther)
	response := h.request(http.MethodGet, "/post/s/toc-review", nil, false)
	requireStatus(t, response, http.StatusOK)
	page := response.Body.String()
	for _, markup := range []string{
		`<h2 id="section-route-timing">Route &amp; timing</h2>`,
		`href="#section-route-timing">Route &amp; timing</a><ul><li><a href="#section-bring-water">Bring water</a>`,
		`<h2 id="section-route-timing-2">`,
		`href="#section-café-hours">Café hours</a>`,
	} {
		if !strings.Contains(page, markup) {
			t.Fatalf("expected %s in the post", markup)
		}
	}
	start := strings.Index(page, `<nav class="post-toc"`)
	if start < 0 {
		t.Fatal("the post must contain a table of contents")
	}
	end := start + strings.Index(page[start:], "</nav>")
	nav := page[start:end]
	if strings.Contains(nav, "Extra detail") || strings.Contains(nav, "Quoted heading") || strings.Contains(nav, `href="/about"`) {
		t.Fatalf("unexpected outline contents: %s", nav)
	}
	feed := h.request(http.MethodGet, "/rss", nil, false)
	requireStatus(t, feed, http.StatusOK)
	if !strings.Contains(feed.Body.String(), "https://blog.example/post/s/toc-review#section-route-timing") {
		t.Fatal("RSS must link to the matching section in the post")
	}
}

func TestTableOfContentsNeedsTwoHeadings(t *testing.T) {
	source, err := joinPostComponents("## First section\n\n## Second section\n\n#### Just one match", componentInput{"tableOfContents": `{"levels":[4]}`})
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := markdown(source)
	if err != nil || strings.Contains(string(rendered), "post-toc") {
		t.Fatalf("one heading must not render a table of contents: %s, %v", rendered, err)
	}
}

func TestTableOfContentsLevelSettings(t *testing.T) {
	for _, tc := range []struct {
		source, saved string
	}{
		{`{}`, `{}`}, {`{"levels":[3,2]}`, `{}`}, {`{"levels":[6,4,1]}`, `{"levels":[1,4,6]}`},
		{`{"levels":[]}`, ""}, {`{"levels":null}`, ""}, {`{"levels":[0,7]}`, ""},
		{`{"levels":[2,2]}`, ""}, {`{"levels":[2.5]}`, ""}, {`{"other":true}`, ""},
	} {
		t.Run(tc.source, func(t *testing.T) {
			settings, err := parseTableOfContents(tc.source)
			if tc.saved == "" {
				if err == nil {
					t.Fatal("invalid levels were accepted")
				}
				return
			}
			if err != nil || settings.EditorSource() != tc.saved {
				t.Fatalf("expected %s, got %+v, %v", tc.saved, settings, err)
			}
		})
	}
}

func TestTableOfContentsSelectedLevels(t *testing.T) {
	const secondID = "table-of-contents:00000000000000000000000000000002"
	body := placementMarker("table-of-contents") + "\n\n# Before leaving\n\n## Equipment\n\n### Essentials\n\n#### Bring water\n\n###### Check the seal\n\n" +
		placementMarker(secondID) + "\n\n## At the trailhead\n\n#### Map check"
	source, err := joinPostComponents(body, componentInput{
		"componentOrder":  componentJSON([]string{"table-of-contents", secondID}),
		"tableOfContents": `{"levels":[1,3,4,6]}`, secondID: `{"levels":[2,4]}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := markdown(source)
	if err != nil {
		t.Fatal(err)
	}
	outlines := strings.Split(string(rendered), `<nav class="post-toc"`)
	if len(outlines) != 3 {
		t.Fatalf("expected two outlines, got %s", rendered)
	}
	first, _, _ := strings.Cut(outlines[1], "</nav>")
	second, _, _ := strings.Cut(outlines[2], "</nav>")
	if !strings.Contains(first, `href="#section-essentials">Essentials</a><ul><li><a href="#section-bring-water">Bring water</a><ul><li><a href="#section-check-the-seal">Check the seal</a>`) ||
		!strings.Contains(first, `</ul></li><li><a href="#section-map-check">Map check</a>`) || strings.Contains(first, "Equipment") {
		t.Fatalf("unexpected first outline: %s", first)
	}
	if !strings.Contains(second, `href="#section-equipment">Equipment</a><ul><li><a href="#section-bring-water">Bring water</a>`) ||
		strings.Contains(second, "Essentials") || strings.Contains(second, "Before leaving") {
		t.Fatalf("unexpected second outline: %s", second)
	}
}

func TestBrowserTableOfContents(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to verify the table of contents")
	}
	for _, viewport := range []struct {
		width  string
		height int
	}{{"390", 850}, {"1440", 600}, {"1920", 1080}, {"1680", 720}, {"2560", 1080}} {
		t.Run(viewport.width, func(t *testing.T) {
			testBrowser(t, browser, viewport.width, browserScenario{
				scriptFile: "testdata/table-of-contents.js", startPath: "/post/create",
				height: viewport.height, authenticated: true,
				setup: func(t *testing.T) *harness {
					h := registeredBrowserHarness(t)
					h.login(t)
					return h
				},
			})
		})
	}
}
