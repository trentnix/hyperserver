package blog

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
)

func TestComparisonComponent(t *testing.T) {
	value := postComparison{BeforeID: strings.Repeat("a", 64) + ".png", AfterID: strings.Repeat("b", 64) + ".jpeg", BeforeLabel: `Original <image> & "source"`, AfterLabel: "Revised", Title: `<script>Title</script>`, Caption: "A comparison & context.\nA second line.", BeforeAlt: `Before "edits"`, AfterAlt: "After edits"}
	source, err := joinPostComponents("Body", componentInput{"comparison": componentJSON(value), "video": `{"url":"https://youtu.be/M7lc1UVf-VE"}`, "componentOrder": `["image-comparison","video"]`})
	if err != nil {
		t.Fatal(err)
	}
	body, components, err := splitPostComponents(source)
	if err != nil || body != "Body" || len(components.Components) != 2 || components.Components[0].Content.EditorSource() != componentJSON(value) {
		t.Fatal("comparison did not round trip", components, err)
	}
	rendered, err := markdown(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{html.EscapeString(value.Title), html.EscapeString(value.Caption), html.EscapeString(value.BeforeAlt), `>` + html.EscapeString(value.BeforeLabel) + `</span>`, `aria-valuetext="` + html.EscapeString("50% "+value.BeforeLabel+", 50% "+value.AfterLabel) + `"`, `type="range"`, "/images/" + value.AfterID + "?variant=display"} {
		if !strings.Contains(string(rendered), want) {
			t.Fatalf("reader missing %q: %s", want, rendered)
		}
	}
	if strings.Contains(string(rendered), "<script>") || strings.Index(string(rendered), "image-comparison") > strings.Index(string(rendered), "video-component") {
		t.Fatal("unsafe content or incorrect component order", rendered)
	}
	feed, err := markdownAt(source, &url.URL{Scheme: "https", Host: "blog.example"})
	if err != nil || strings.Count(string(feed), "<img") != 2 || !strings.Contains(string(feed), "https://blog.example/images/"+value.BeforeID) || !strings.Contains(string(feed), html.EscapeString(value.BeforeLabel)) || strings.Contains(string(feed), `type="range"`) || strings.Contains(string(feed), "comparison-divider") {
		t.Fatal("feed must retain both absolute image URLs without controls", feed, err)
	}
	refs, err := contentMediaReferences(source, "/post/s/comparison", "")
	if err != nil || len(refs) != 2 || !slices.Contains(refs, "/images/"+value.BeforeID) || !slices.Contains(refs, "/images/"+value.AfterID) {
		t.Fatal("both images must retain their references", refs, err)
	}
	search := searchableWriting(source)
	if !strings.Contains(search, value.Caption) || !strings.Contains(search, value.AfterAlt) || !strings.Contains(search, value.BeforeLabel) || strings.Contains(search, value.BeforeID) {
		t.Fatal("search must use authored text", search)
	}
	for _, raw := range []string{
		`null`, `{}`, `{"beforeID":"` + value.BeforeID + `"}`, `{"beforeID":"/images/` + value.BeforeID + `","afterID":"` + value.AfterID + `"}`,
		`{"beforeID":"` + value.BeforeID + `","afterID":"` + value.AfterID + `","html":"bad"}`, componentJSON(postComparison{BeforeID: value.BeforeID, AfterID: value.BeforeID}),
		componentJSON(postComparison{BeforeID: value.BeforeID, AfterID: value.AfterID, Title: strings.Repeat("a", 241)}),
		componentJSON(postComparison{BeforeID: value.BeforeID, AfterID: value.AfterID, Caption: strings.Repeat("a", 1001)}),
		componentJSON(postComparison{BeforeID: value.BeforeID, AfterID: value.AfterID, AfterAlt: strings.Repeat("a", 501)}),
		componentJSON(postComparison{BeforeID: value.BeforeID, AfterID: value.AfterID, BeforeLabel: strings.Repeat("界", 81)}),
		componentJSON(postComparison{BeforeID: value.BeforeID, AfterID: value.AfterID, AfterLabel: strings.Repeat("a", 81)}),
	} {
		if _, err := joinPostComponents("Body", componentInput{"comparison": raw}); err == nil {
			t.Fatal("invalid comparison accepted", raw)
		}
	}
	if _, err := joinPostComponents("Body", componentInput{"componentOrder": `["image-comparison"]`}); !slices.Equal(validationFields(err), []string{"comparison"}) {
		t.Fatal("an empty comparison must identify its field", err)
	}
}

func TestComparisonLabels(t *testing.T) {
	for _, test := range []struct {
		name, before, after, wantBefore, wantAfter string
	}{
		{"existing content", "", "", "Before", "After"},
		{"blank labels", " \t", "\n ", "Before", "After"},
		{"custom labels", ` Original <image> `, "Revised", "Original <image>", "Revised"},
		{"maximum label", strings.Repeat("界", 80), "", strings.Repeat("界", 80), "After"},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := postComparison{BeforeID: strings.Repeat("a", 64) + ".png", AfterID: strings.Repeat("b", 64) + ".jpeg", BeforeLabel: test.before, AfterLabel: test.after}
			source, err := joinPostComponents("", componentInput{"comparison": componentJSON(value)})
			if err != nil {
				t.Fatal(err)
			}
			rendered, err := markdown(source)
			if err != nil {
				t.Fatal(err)
			}
			for _, label := range []string{test.wantBefore, test.wantAfter} {
				for _, want := range []string{`alt="` + html.EscapeString(label) + `"`, `>` + html.EscapeString(label) + `</span>`} {
					if !strings.Contains(string(rendered), want) {
						t.Fatalf("missing label or fallback description %q: %s", want, rendered)
					}
				}
			}
		})
	}
}

func TestComparisonFormsHistoryAndMedia(t *testing.T) {
	for _, hx := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "htmx"}[hx], func(t *testing.T) {
			h := registeredBrowserHarness(t)
			h.login(t)
			value := postComparison{BeforeID: mediaUpload(t, h.blog, "Before").ID, AfterID: mediaUpload(t, h.blog, "After").ID, BeforeLabel: "Original", AfterLabel: "Revised", Caption: "Keep this comparison."}
			fields := url.Values{"title": {""}, "slug": {"comparison-review"}, "comparison": {componentJSON(value)}, "componentOrder": {`["image-comparison"]`}}
			rejected := h.request("POST", "/post", fields, hx)
			requireStatus(t, rejected, http.StatusUnprocessableEntity)
			if !hx && !strings.Contains(html.UnescapeString(rejected.Body.String()), componentJSON(value)) {
				t.Fatal("validation discarded the comparison")
			}
			fields.Set("title", "Comparison review")
			missing := value
			missing.AfterID = strings.Repeat("c", 64) + ".png"
			fields.Set("comparison", componentJSON(missing))
			requireStatus(t, h.request("POST", "/post", fields, hx), http.StatusUnprocessableEntity)
			fields.Set("comparison", componentJSON(value))
			status := http.StatusSeeOther
			if hx {
				status = http.StatusOK
			}
			requireStatus(t, h.request("POST", "/post", fields, hx), status)
			post, err := h.blog.posts.GetBySlug(t.Context(), "comparison-review", true)
			if err != nil {
				t.Fatal(err)
			}
			public := func(id string) int {
				response := httptest.NewRecorder()
				h.app.Handler.ServeHTTP(response, httptest.NewRequest("GET", "https://blog.example/images/"+id+"?variant=display", nil))
				return response.Code
			}
			for _, id := range []string{value.BeforeID, value.AfterID} {
				if public(id) != http.StatusNotFound {
					t.Fatal("draft comparison exposed an image")
				}
			}
			versions, err := h.blog.history.ListRevisions(t.Context(), "post", post.ID)
			if err != nil || len(versions) != 1 {
				t.Fatal("comparison revision missing", versions, err)
			}
			original, err := h.blog.history.GetRevision(t.Context(), "post", post.ID, versions[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			post, err = setPostPublication(h.blog.posts, t.Context(), post.ID, true)
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{value.BeforeID, value.AfterID} {
				if public(id) != http.StatusOK {
					t.Fatal("published comparison lost an image reference")
				}
			}
			post.Markdown, post.Draft = "Comparison removed", true
			post, err = savePostFixture(h.blog.posts, t.Context(), post)
			if err != nil {
				t.Fatal(err)
			}
			usage, err := h.blog.mediaReferences.ReferencedMedia(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{value.BeforeID, value.AfterID} {
				if public(id) != http.StatusNotFound || usage[id].Current || len(usage[id].History) == 0 {
					t.Fatal("removed comparison must retain history without public access", usage[id])
				}
			}
			if err := h.blog.restoreRevisionContent(adminWritingContext(t.Context()), "post", post.ID, post.Version, original); err != nil {
				t.Fatal(err)
			}
			restored, err := h.blog.posts.GetByID(t.Context(), post.ID, true)
			if err != nil || restored.Markdown != original.Markdown {
				t.Fatal("restoring history lost the comparison", restored, err)
			}
		})
	}
}

func TestComparisonPosterCannotUsePrivateImages(t *testing.T) {
	h := usersHarness(t)
	admin, err := h.app.Accounts.GetByEmail(t.Context(), "trentnix@gmail.com")
	if err != nil {
		t.Fatal(err)
	}
	before := mediaUpload(t, h.blog, "Private before").ID
	after := mediaUpload(t, h.blog, "Private after").ID
	for _, id := range []string{before, after} {
		if err := h.blog.mediaMetadata.RememberMedia(t.Context(), id, mediaMetadata{Filename: "private.png", UploadedBy: admin.ID}); err != nil {
			t.Fatal(err)
		}
	}
	poster := registeredPoster(t, h)
	fields := url.Values{"title": {"Comparison"}, "slug": {"poster-comparison"}, "comparison": {componentJSON(postComparison{BeforeID: before, AfterID: after})}, "componentOrder": {`["image-comparison"]`}}
	for _, hx := range []bool{false, true} {
		requireStatus(t, h.request("POST", "/post", fields, hx), http.StatusUnprocessableEntity)
	}
	// Ownership of both images, rather than only the visible side, is required.
	before = mediaUpload(t, h.blog, "Poster before").ID
	if err := h.blog.mediaMetadata.RememberMedia(t.Context(), before, mediaMetadata{Filename: "before.png", UploadedBy: poster.ID}); err != nil {
		t.Fatal(err)
	}
	fields.Set("comparison", componentJSON(postComparison{BeforeID: before, AfterID: after}))
	requireStatus(t, h.request("POST", "/post", fields, false), http.StatusUnprocessableEntity)
	after = mediaUpload(t, h.blog, "Poster after").ID
	if err := h.blog.mediaMetadata.RememberMedia(t.Context(), after, mediaMetadata{Filename: "after.png", UploadedBy: poster.ID}); err != nil {
		t.Fatal(err)
	}
	fields.Set("comparison", componentJSON(postComparison{BeforeID: before, AfterID: after}))
	requireStatus(t, h.request("POST", "/post", fields, false), http.StatusSeeOther)
}
