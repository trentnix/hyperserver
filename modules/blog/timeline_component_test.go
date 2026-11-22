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

func TestTimelineDates(t *testing.T) {
	for input, display := range map[string]string{
		"2023": "2023", "2024-05": "May 2024", "2025-10-18": "Oct 18, 2025", "2024-02-29": "Feb 29, 2024",
		"0001-01": "Jan 0001", "": "", "0000": "", "2024-1": "", "2024-01-1": "", "2025-02-29": "",
		"1900-02-29": "", "2024-04-31": "", "2024-13": "", "2024-00": "", "2024-01-00": "", "2024-01-01T00:00": "",
	} {
		if got := timelineDateLabel(input); got != display {
			t.Errorf("timelineDateLabel(%q) = %q, want %q", input, got, display)
		}
	}
}

func TestTimelineValidation(t *testing.T) {
	item := timelineMilestone{Date: "2024", Title: "Prototype"}
	tooMany := make([]timelineMilestone, maxTimelineMilestones+1)
	for i := range tooMany {
		tooMany[i] = item
	}
	for _, source := range []string{
		"", "null", `{"milestones":[]}`, `{"milestones":[{"date":"2024","title":"Prototype","extra":true}]}`,
		componentJSON(postTimeline{Milestones: tooMany}),
		componentJSON(postTimeline{Title: strings.Repeat("x", 241), Milestones: []timelineMilestone{item}}),
		componentJSON(postTimeline{Style: "custom", Milestones: []timelineMilestone{item}}),
		componentJSON(postTimeline{Milestones: []timelineMilestone{{Date: "2023-02-29", Title: item.Title}}}),
		componentJSON(postTimeline{Milestones: []timelineMilestone{{Date: item.Date, Title: " "}}}),
		componentJSON(postTimeline{Milestones: []timelineMilestone{{Date: item.Date, Title: item.Title, Detail: strings.Repeat("x", maxMilestoneDetailRunes+1)}}}),
	} {
		_, err := joinPostComponents("Body", componentInput{"timeline": source, "componentOrder": `["timeline"]`})
		if !slices.Contains(validationFields(err), "timeline") {
			t.Fatalf("invalid component did not identify the timeline field: %v", err)
		}
	}
}

func TestTimelinePalettes(t *testing.T) {
	for _, style := range []string{"", "white", "parchment", "forest", "brick"} {
		source, err := joinPostComponents("Body", componentInput{
			"timeline":       componentJSON(postTimeline{Style: style, Milestones: []timelineMilestone{{Date: "2025", Title: "First version"}}}),
			"componentOrder": `["timeline"]`,
		})
		if err != nil {
			t.Fatal(err)
		}
		rendered, err := markdown(source)
		if err != nil {
			t.Fatal(err)
		}
		if style == "" && strings.Contains(string(rendered), "data-timeline-style") ||
			style != "" && !strings.Contains(string(rendered), `data-timeline-style="`+style+`"`) {
			t.Errorf("style %q rendered as %s", style, rendered)
		}
	}
}

func TestTimelinePostAndFeed(t *testing.T) {
	h := registeredBrowserHarness(t)
	h.login(t)
	value := postTimeline{Title: " Project <milestones> ", Milestones: []timelineMilestone{
		{Date: " 2025-10-18 ", Title: " Version 1.0 ", Detail: "**Released** with a [handout](handout).\n\n<script>alert(1)</script>"},
		{Date: "2025-10", Title: "October preview"},
		{Date: "2025", Title: "Planning"},
		{Date: "2023", Title: "First prototype"},
		{Date: "2025-10-18", Title: "Release notes"},
	}}
	fields := url.Values{
		"title": {"Timeline review"}, "slug": {"timeline-review"}, "published": {"on"},
		"content":        {"Before milestones.\n\n" + placementMarker("timeline") + "\n\nAfter milestones."},
		"componentOrder": {`["timeline"]`},
		"timeline":       {componentJSON(value)},
	}
	requireStatus(t, h.request(http.MethodPost, "/post", fields, false), http.StatusSeeOther)
	post, err := h.blog.posts.GetBySlug(t.Context(), "timeline-review", false)
	if err != nil {
		t.Fatal(err)
	}
	_, components, err := splitPostComponents(post.Markdown)
	if err != nil || len(components.Components) != 1 {
		t.Fatal("timeline did not persist", err)
	}
	saved := components.Components[0].Content.(*postTimeline)
	if saved.Milestones[0].Date != "2025-10-18" || saved.Milestones[0].Title != "Version 1.0" || saved.Milestones[3].Date != "2023" {
		t.Fatal("editor order or normalized fields changed", saved)
	}
	if !strings.Contains(searchableWriting(post.Markdown), "Released") {
		t.Fatal("search omitted timeline detail")
	}
	clear(h.cookies)

	page := h.request(http.MethodGet, "/post/s/timeline-review", nil, false)
	requireStatus(t, page, http.StatusOK)
	body := page.Body.String()
	ordered := []string{
		"Before milestones.", "<h3>Project &lt;milestones&gt;</h3>",
		`<time datetime="2023">2023</time>`, "<strong>First prototype</strong>",
		`<time datetime="2025">2025</time>`, "<strong>Planning</strong>",
		`<time datetime="2025-10">Oct 2025</time>`, "<strong>October preview</strong>",
		`<time datetime="2025-10-18">Oct 18, 2025</time>`, "<strong>Version 1.0</strong>",
		"<strong>Released</strong>", "<strong>Release notes</strong>", "After milestones.",
	}
	for _, expected := range ordered {
		_, remaining, ok := strings.Cut(body, expected)
		if !ok {
			t.Fatalf("missing or out of order: %q", expected)
		}
		body = remaining
	}
	if strings.Contains(page.Body.String(), "<script>alert(1)</script>") {
		t.Fatal("raw HTML was emitted from a Markdown detail")
	}

	response := h.request(http.MethodGet, "/rss", nil, false)
	requireStatus(t, response, http.StatusOK)
	var rss feed
	if err := xml.Unmarshal(response.Body.Bytes(), &rss); err != nil || len(rss.Channel.Items) != 1 {
		t.Fatalf("expected one feed item: %v, %s", err, response.Body)
	}
	item := rss.Channel.Items[0].Content
	if !strings.Contains(item, `<time datetime="2025-10">Oct 2025</time>`) ||
		!strings.Contains(item, `href="https://blog.example/post/s/handout"`) {
		t.Fatalf("RSS must keep dates and resolve detail links: %s", item)
	}
}

func TestTimelineDetailImageReference(t *testing.T) {
	image := strings.Repeat("a", 64) + ".png"
	source, err := joinPostComponents("Body", componentInput{
		"timeline":       componentJSON(postTimeline{Milestones: []timelineMilestone{{Date: "2025", Title: "First version", Detail: "![Screenshot](/images/" + image + ")"}}}),
		"componentOrder": `["timeline"]`,
	})
	if err != nil {
		t.Fatal(err)
	}
	refs, err := contentMediaReferences(source, "/post/s/example", "")
	if err != nil || !slices.Equal(refs, []string{"/images/" + image}) {
		t.Fatal("timeline images must count as media references", refs, err)
	}
}

func TestBrowserTimeline(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check Timeline")
	}
	for _, width := range []string{"320", "1100"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/timeline.js", startPath: "/pages/create", authenticated: true,
				setup: func(t *testing.T) *harness {
					h := registeredBrowserHarness(t)
					h.login(t)
					return h
				},
			})
		})
	}
}
