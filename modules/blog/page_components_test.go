package blog

import (
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestPageComponentsSaveAndRestore(t *testing.T) {
	h := registeredBrowserHarness(t)
	h.login(t)
	file := testDownload(t, h, "worksheet.txt", "Page component download")
	filePath := "/files/" + file.ID
	public := func(path string, status int) string {
		t.Helper()
		response := httptest.NewRecorder()
		h.app.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "https://blog.example"+path, nil))
		requireStatus(t, response, status)
		return response.Body.String()
	}

	fields := pageFields("component-guide", false)
	order := `["table-of-contents","callout","file-downloads"]`
	body := placementMarker("table-of-contents") + "\n\n## Start here\n\n" + placementMarker("callout") +
		"\n\n#### Read more\n\n[Directions](directions)"
	fields.Set("content", body)
	fields.Set("componentOrder", order)
	fields.Set("tableOfContents", `{"levels":[2,4]}`)
	fields.Set("downloads", downloadSource(t, file))
	for _, hx := range []bool{false, true} {
		response := h.request(http.MethodPost, "/pages", fields, hx)
		requireStatus(t, response, http.StatusUnprocessableEntity)
		if !strings.Contains(response.Body.String(), `data-error-fields="callout"`) || response.Header().Get("HX-Trigger") != "" {
			t.Fatal("empty callout must identify its field without reporting a save")
		}
		if !hx && (!strings.Contains(response.Body.String(), "## Start here") || !strings.Contains(html.UnescapeString(response.Body.String()), fields.Get("downloads"))) {
			t.Fatal("ordinary validation error lost submitted text or components")
		}
	}
	fields.Set("callout", `{"body":"Use the **worksheet**.","style":"slate"}`)
	requireStatus(t, h.request(http.MethodPost, "/pages", fields, false), http.StatusSeeOther)
	draft, err := h.blog.pageStore.GetPageBySlug(t.Context(), "component-guide", true)
	if err != nil {
		t.Fatal(err)
	}
	versions, err := h.blog.history.ListRevisions(t.Context(), "page", draft.ID)
	if err != nil || len(versions) != 1 {
		t.Fatalf("expected one saved writing revision, got %d, %v", len(versions), err)
	}
	revision := strconv.FormatInt(versions[0].ID, 10)
	public(draft.URL(), http.StatusNotFound)
	public(filePath, http.StatusNotFound)

	fields.Set("version", strconv.FormatInt(draft.Version, 10))
	fields.Set("published", "on")
	requireStatus(t, h.request(http.MethodPost, "/pages/"+draft.ID, fields, true), http.StatusOK)
	reader := public(draft.URL(), http.StatusOK)
	start := strings.Index(reader, `<h2 id="section-start-here">`)
	note := strings.Index(reader, `data-callout-style="slate"`)
	finish := strings.Index(reader, `<h4 id="section-read-more">`)
	if start < 0 || note <= start || finish <= note || !strings.Contains(reader, `href="#section-read-more"`) || !strings.Contains(reader, `href="`+filePath+`"`) {
		t.Fatal("published page did not render the saved components and placement")
	}
	public(filePath, http.StatusOK)

	plain := pageFields("component-guide", true)
	plain.Set("content", "Current page text without uploads.")
	requireStatus(t, h.submitEditor(t, "/pages/"+draft.ID, plain, false), http.StatusSeeOther)
	public(filePath, http.StatusNotFound)
	history := h.request(http.MethodGet, "/history/page/"+draft.ID+"?revision="+revision, nil, false)
	requireStatus(t, history, http.StatusOK)
	preview := history.Body.String()
	if !strings.Contains(preview, `href="#section-read-more"`) || !strings.Contains(preview, `id="section-read-more"`) ||
		!strings.Contains(preview, `href="/page/directions"`) {
		t.Fatal("history links must jump within the preview and resolve other relative links against the page")
	}
	editor := h.request(http.MethodGet, "/pages/update/"+draft.ID+"?revision="+revision, nil, false)
	requireStatus(t, editor, http.StatusOK)
	data := regexp.MustCompile(`data-current-content="([^"]+)"`).FindStringSubmatch(editor.Body.String())
	var baseline map[string]string
	if len(data) != 2 || json.Unmarshal([]byte(html.UnescapeString(data[1])), &baseline) != nil || baseline["content"] != plain.Get("content") || baseline["componentOrder"] != "[]" {
		t.Fatal("editor did not load current writing as the historical edit baseline")
	}
	if !strings.Contains(editor.Body.String(), `data-component-add="expandable-details"`) || !strings.Contains(html.UnescapeString(editor.Body.String()), fields.Get("tableOfContents")) {
		t.Fatal("historical page editor did not reopen the saved components")
	}
	current, err := h.blog.pageStore.GetPageByID(t.Context(), draft.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	restore := "/history/page/" + draft.ID + "/restore?revision=" + revision
	requireStatus(t, h.request(http.MethodPost, restore, url.Values{"version": {strconv.FormatInt(current.Version, 10)}}, false), http.StatusSeeOther)
	if !strings.Contains(public(draft.URL(), http.StatusOK), `<nav class="post-toc"`) {
		t.Fatal("restoring the revision did not restore the outline")
	}
	public(filePath, http.StatusOK)
}

func TestPageRelatedPostValidation(t *testing.T) {
	h := registeredBrowserHarness(t)
	h.login(t)
	fields := pageFields("page-related-test", true)
	fields.Set("componentOrder", `["related-posts"]`)
	fields.Set("related", componentJSON(postRelated{Posts: []string{strings.Repeat("A", 26)}}))
	response := h.request(http.MethodPost, "/pages", fields, true)
	requireStatus(t, response, http.StatusUnprocessableEntity)
	if !strings.Contains(response.Body.String(), `data-error-fields="related"`) || !strings.Contains(response.Body.String(), "A selected post is unavailable") {
		t.Fatal("a new page reference must resolve to a valid post")
	}
}

func TestBrowserPageComponents(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check page components")
	}
	testBrowser(t, browser, "390", browserScenario{
		scriptFile: "testdata/page-components.js", startPath: "/pages/create", authenticated: true,
		setup: func(t *testing.T) *harness {
			h := registeredBrowserHarness(t)
			h.login(t)
			return h
		},
	})
}

func TestBrowserComponentsWithoutEditorScript(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check the native content forms")
	}
	const slug = "native-editor-review"
	pageBody := "Original writing.\n\n" + placementMarker("callout")
	for _, tc := range []struct{ name, path, body, missingScript string }{
		{"page", "/page/" + slug, pageBody, "/assets/js/admin.js"},
		{"post", "/post/s/" + slug, "", "/assets/js/admin.js"},
		{"page_without_easymde", "/page/" + slug, pageBody, "/assets/lib/easymde/easymde.min.js"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testBrowser(t, browser, "390", browserScenario{
				scriptFile: "testdata/components-native.js", startPath: tc.path, authenticated: true,
				setup: func(t *testing.T) *harness {
					h := registeredBrowserHarness(t)
					h.login(t)
					source, err := joinPostComponents(tc.body, componentInput{"callout": `{"body":"Keep this callout."}`})
					if err != nil {
						t.Fatal(err)
					}
					if tc.body != "" {
						_, err = h.blog.persistPage(t.Context(), Page{Slug: slug, Title: "Native editor review", Markdown: source, Published: true, ChildrenDisplay: "hidden"})
					} else {
						_, err = h.blog.persistPost(t.Context(), Post{Slug: slug, Title: "Native editor review", Markdown: source})
					}
					if err != nil {
						t.Fatal(err)
					}
					return h
				},
				routes: func(*harness) map[string]http.HandlerFunc {
					emptyScript := func(w http.ResponseWriter, _ *http.Request) { w.Header().Set("Content-Type", "text/javascript") }
					return map[string]http.HandlerFunc{
						tc.missingScript:               emptyScript,
						"/assets/lib/htmx/htmx.min.js": emptyScript,
					}
				},
			})
		})
	}
}

func TestBrowserPageOutline(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check the ultrawide page outline")
	}
	for _, display := range []string{"list", "blocks"} {
		t.Run(display, func(t *testing.T) {
			testBrowser(t, browser, "2560", browserScenario{
				scriptFile: "testdata/page-outline.js", startPath: "/page/outline-review", height: 720,
				setup: func(t *testing.T) *harness {
					h := registeredBrowserHarness(t)
					body := placementMarker("table-of-contents") + "\n\n## Prepare\n\n## Finish"
					source, err := joinPostComponents(body, componentInput{"tableOfContents": "{}"})
					if err != nil {
						t.Fatal(err)
					}
					_, err = h.blog.persistPage(t.Context(), Page{
						Slug: "outline-review", Title: "Outline review", Markdown: source, Published: true, ChildrenDisplay: display,
					})
					if err != nil {
						t.Fatal(err)
					}
					return h
				},
			})
		})
	}
}
