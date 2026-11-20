package blog

import (
	"encoding/xml"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
)

func placementMarker(kind string) string { return componentMarkerPrefix + kind + componentMarkerSuffix }

func TestComponentLayoutMarkers(t *testing.T) {
	marker := placementMarker("embed")
	for _, tc := range []struct {
		body       string
		markerLine int
	}{
		{"<!-- opening\n" + marker + "\nrest of the comment\n-->\n", -1},
		{"<div>\n</div>\n" + marker, -1},
		{"<div>\n\n" + marker + "\n\nConclusion.", 2},
		{"Intro.\n\n```md\n" + marker + "\n```\n\n" + marker, 6},
	} {
		layout := layoutComponents(tc.body)
		line, found := layout.Markers["embed"]
		if found != (tc.markerLine >= 0) || found && line != tc.markerLine {
			t.Errorf("markers for %q = %v, want embed at %d", tc.body, layout.Markers, tc.markerLine)
		}
	}
	for _, body := range []string{"```md\nunfinished fence", "<!-- unfinished comment", "<script>\nunfinished raw HTML block"} {
		if layout := layoutComponents(body); layout.End != nil {
			t.Errorf("found a destination at the end of %q", body)
		}
	}
}

func TestComponentPlacementAfterHTML(t *testing.T) {
	for _, body := range []string{"<div>\n</div>", "<div>\n</div>\n", "<div>\n</div>\n\n"} {
		layout := layoutComponents(body)
		if layout.End == nil || *layout.End != 3 || !slices.Contains(layout.Drops, 3) {
			t.Fatalf("layout for %q has wrong end or drop lines: %+v", body, layout)
		}
		prefix := strings.Repeat("\n", *layout.End-strings.Count(body, "\n"))
		if line, ok := layoutComponents(body + prefix + placementMarker("embed")).Markers["embed"]; !ok || line != *layout.End {
			t.Errorf("the component after %q was not recognized at line %d", body, *layout.End)
		}
	}
}

func TestPlacedComponentsRenderAndRoundTrip(t *testing.T) {
	marker := placementMarker("embed")
	const after, before = "After the embed.", "Before the embed."
	body := before + "\n\n```md\n" + marker + "\n```\n\n" + marker + "\n\n" + after
	input := componentInput{
		"embed":          componentJSON(postEmbed{URL: testEmbedURL, Title: "Example"}),
		"video":          `{"url":"https://youtu.be/M7lc1UVf-VE","title":"At the end"}`,
		"componentOrder": `["video","embed"]`,
	}
	source, err := joinPostComponents(body, input)
	if err != nil {
		t.Fatal(err)
	}
	back, components, err := splitPostComponents(source)
	if err != nil || back != body || components.Version != 3 || !slices.Equal(components.Order(), []string{"video", "embed"}) {
		t.Fatalf("placed component round trip failed: %v, %v", components, err)
	}
	checkOrder := func(html string, want ...string) {
		t.Helper()
		for _, text := range want {
			i := strings.Index(html, text)
			if i < 0 {
				t.Fatalf("missing %q in the expected order: %s", text, html)
			}
			html = html[i+len(text):]
		}
	}
	legacy := strings.Replace(source, `"version":3`, `"version":2`, 1)
	rendered, err := markdown(legacy)
	if err != nil {
		t.Fatal(err)
	}
	checkOrder(string(rendered), before, after, "At the end", "Open Example")
	rendered, err = markdown(source)
	if err != nil {
		t.Fatal(err)
	}
	checkOrder(string(rendered), before, "&lt;!-- hyperserver:component embed --&gt;", "Open Example", after, "At the end")
	if strings.Count(searchableWriting(source), marker) != 1 {
		t.Fatal("search must omit the placement marker and preserve the code example")
	}
	for _, invalid := range []string{marker + "\n" + marker, placementMarker("unknown")} {
		if _, err := joinPostComponents(invalid, input); !slices.Equal(validationFields(err), []string{"content"}) {
			t.Fatalf("placement error must identify the content field: %v", err)
		}
	}
}

func TestComponentDropLines(t *testing.T) {
	for _, tc := range []struct {
		body string
		want []int
	}{
		{"", []int{0}},
		{"Intro.\n\n- First\n- Second\n\nConclusion.", []int{0, 1, 2, 4, 5, 6}},
		{"Intro.\n\n```md\nExample\n\n```\n\nConclusion.\n", []int{0, 1, 2, 6, 7, 8}},
		{"Intro.\n\n```md\nUnfinished\n\n", []int{0, 1, 2}},
		{"<div>\nRaw HTML.\n\n\nWriting.", []int{0, 3, 4, 5}},
		{"<div>\nRaw HTML.\n\n", []int{0, 3}},
		{"Intro.\n" + placementMarker("embed") + "\nConclusion.", []int{0, 1, 2, 3}},
		{"Intro.\n\n\n" + placementMarker("embed") + "\n## Next section\n\nConclusion.", []int{0, 1, 2, 3, 4, 5, 6, 7}},
	} {
		got := layoutComponents(tc.body).Drops
		if !slices.Equal(got, tc.want) {
			t.Errorf("drop lines for %q = %v, want %v", tc.body, got, tc.want)
		}
	}
}

func TestComponentRemovalSeparators(t *testing.T) {
	marker := placementMarker("embed")
	for _, tc := range []struct {
		before, after, separator string
		keep                     bool
	}{
		{"Introduction.", "Second paragraph.", "", true},
		{"Introduction.", "---", "", true},
		{"- First list\n", "\n- Second list", "<!-- -->", true},
		{"- First list", "    stand-alone code", "<!-- -->", true},
		{"    first code block", "    second code block", "<!-- -->", true},
		{"Introduction.\n", "Second paragraph.", "", false},
		{"Introduction.", placementMarker("video") + "\nSecond paragraph.", "", false},
	} {
		body := tc.before + "\n" + marker + "\n" + tc.after
		got, keep := layoutComponents(body).Separators["embed"]
		if got != tc.separator || keep != tc.keep {
			t.Errorf("separator for %q = %q, %t, want %q, %t", body, got, keep, tc.separator, tc.keep)
		}
	}
}

func TestPlacedComponentsPreviewAndFeed(t *testing.T) {
	h := embedTestHarness(t)
	h.login(t)
	fields := url.Values{
		"title": {"Placed embed"}, "slug": {"placed-embed"}, "published": {"on"},
		"content": {"Introduction.\n\n" + placementMarker("embed") + "\n\nConclusion."},
		"embed":   {componentJSON(postEmbed{URL: testEmbedURL, Title: "Placed chart"})}, "componentOrder": {`["embed"]`},
	}
	check := func(body string) {
		t.Helper()
		first, embed, last := strings.Index(body, "Introduction."), strings.Index(body, "Open Placed chart"), strings.Index(body, "Conclusion.")
		if first < 0 || embed <= first || last <= embed {
			t.Fatalf("embed rendered out of place: %s", body)
		}
	}
	preview := h.request("POST", "/post/preview", fields, false)
	requireStatus(t, preview, http.StatusOK)
	check(preview.Body.String())
	requireStatus(t, h.request("POST", "/post", fields, false), http.StatusSeeOther)
	page := h.request("GET", "/post/s/placed-embed", nil, false)
	requireStatus(t, page, http.StatusOK)
	check(page.Body.String())
	response := h.request("GET", "/rss", nil, false)
	requireStatus(t, response, http.StatusOK)
	var rss feed
	if err := xml.Unmarshal(response.Body.Bytes(), &rss); err != nil || len(rss.Channel.Items) != 1 {
		t.Fatalf("expected the published post in the feed: %v", err)
	}
	check(rss.Channel.Items[0].Content)
}

func TestRepeatedComponents(t *testing.T) {
	extra := "embed:" + strings.Repeat("a", 32)
	input := componentInput{
		"embed":          componentJSON(postEmbed{URL: testEmbedURL, Title: "First chart"}),
		extra:            componentJSON(postEmbed{URL: testEmbedURL, Title: "Second chart"}),
		"componentOrder": componentJSON([]string{extra, "embed"}),
	}
	body := "Opening.\n\n" + placementMarker("embed") + "\n\nMiddle.\n\n" + placementMarker(extra) + "\n\nClosing."
	source, err := joinPostComponents(body, input)
	if err != nil {
		t.Fatal(err)
	}
	writing, components, err := splitPostComponents(source)
	if err != nil || writing != body || components.Version != 4 || !slices.Equal(components.Order(), []string{extra, "embed"}) {
		t.Fatalf("could not round-trip repeated components: %v, %v", components, err)
	}
	output, err := markdown(source)
	if err != nil || !strings.Contains(string(output), "Second chart") || strings.Index(string(output), "First chart") >= strings.Index(string(output), "Middle.") ||
		strings.Index(string(output), "Middle.") >= strings.Index(string(output), "Second chart") {
		t.Fatalf("rendered components at the wrong locations: %s, %v", output, err)
	}
	extras := (editorPage{Post: Post{Markdown: source}}).ExtraComponents()
	if extras[extra] != input[extra] || len(extras) != 1 {
		t.Fatalf("editor lost instance data: %+v", extras)
	}
	if _, _, err := splitPostComponents(strings.Replace(source, `"type":"embed"`, `"type":"video"`, 1)); err == nil {
		t.Fatal("accepted an instance ID for a different component kind")
	}
	input[extra] = ""
	if _, err := joinPostComponents(body, input); !slices.Equal(validationFields(err), []string{extra}) {
		t.Fatalf("empty repeat must identify its own editor: %v", err)
	}
}

func TestComponentPositionAcceptsDraftOverSaveLimit(t *testing.T) {
	h := embedTestHarness(t)
	h.login(t)
	body := strings.Repeat("x", maxMarkdownBytes) + "\n\n" + placementMarker("embed")
	response := h.request("POST", "/post/component-position", url.Values{"content": {body}}, false)
	requireStatus(t, response, http.StatusOK)
	if !strings.Contains(response.Body.String(), `"embed":2`) {
		t.Fatalf("could not locate the marker in a draft over the save limit: %s", response.Body.String())
	}
}
