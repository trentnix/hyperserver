package blog

import (
	"html"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func testPDF(t *testing.T, h *harness) postDownload {
	t.Helper()
	data, err := os.ReadFile("testdata/preview.pdf")
	if err != nil {
		t.Fatal(err)
	}
	return testDownload(t, h, "preview.pdf", string(data))
}

func TestPDFComponent(t *testing.T) {
	file := postDownload{ID: strings.Repeat("a", 64) + ".pdf", Label: `A <document> "title"`, Description: "A <script>description</script>", Size: 1200}
	source, err := joinPostComponents("Body", componentInput{"componentOrder": `["pdf-preview"]`, "pdf": downloadSource(t, file)})
	if err != nil {
		t.Fatal(err)
	}
	body, components, err := splitPostComponents(source)
	if err != nil || body != "Body" || !slices.Equal(components.PDF(), []postDownload{file}) {
		t.Fatal("PDF did not round trip", components, err)
	}
	rendered, err := markdown(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{`class="pdf-preview"`, `<iframe`, `?preview=1`, `title="` + html.EscapeString(file.Label) + `"`, html.EscapeString(file.Description), ` download `} {
		if !strings.Contains(string(rendered), part) {
			t.Fatal("missing PDF markup", part, rendered)
		}
	}
	if strings.Contains(string(rendered), "<script>") {
		t.Fatal("unescaped component text")
	}
	refs, err := contentMediaReferences(source, "/post/s/pdf", "")
	if err != nil || !slices.Equal(refs, []string{"/files/" + file.ID}) {
		t.Fatal("preview and download must share one canonical reference", refs, err)
	}
	feed, err := markdownAt(source, &url.URL{Scheme: "https", Host: "blog.example", Path: "/post/s/pdf"})
	if err != nil || strings.Contains(string(feed), "iframe") || !strings.Contains(string(feed), "https://blog.example/files/"+file.ID+"?preview=1") || !strings.Contains(string(feed), "Download") {
		t.Fatal("feed needs absolute links without a viewer", feed, err)
	}
	search := searchableWriting(source)
	if !strings.Contains(search, file.Label) || !strings.Contains(search, file.Description) || strings.Contains(search, file.ID) {
		t.Fatal("PDF search text", search)
	}
	for _, invalid := range []string{
		downloadSource(t, postDownload{ID: strings.Repeat("b", 64) + ".txt", Label: "Not a PDF"}),
		downloadSource(t, file, postDownload{ID: strings.Repeat("b", 64) + ".pdf", Label: "Second PDF"}),
		`[{"id":"https://external.example/file.pdf","label":"External"}]`,
	} {
		if _, err := joinPostComponents("Body", componentInput{"pdf": invalid}); err == nil {
			t.Fatal("invalid PDF component accepted", invalid)
		}
	}
}

func TestPDFDownloadLink(t *testing.T) {
	for _, hidden := range []bool{false, true} {
		t.Run(map[bool]string{false: "visible by default", true: "hidden"}[hidden], func(t *testing.T) {
			file := postDownload{ID: strings.Repeat("a", 64) + ".pdf", Label: "Document", HideDownload: hidden}
			source, err := joinPostComponents("Body", componentInput{"pdf": downloadSource(t, file)})
			if err != nil {
				t.Fatal(err)
			}
			_, components, err := splitPostComponents(source)
			if err != nil || !slices.Equal(components.PDF(), []postDownload{file}) {
				t.Fatal("download link setting did not round trip", components, err)
			}
			if !hidden && strings.Contains(source, "hideDownload") {
				t.Fatal("default must use the original saved format")
			}
			page := editorPage{Post: Post{Markdown: source}}
			if page.PDFJSON() != downloadSource(t, file) {
				t.Fatal("editor discarded the download link setting", page.PDFJSON())
			}
			rendered, err := markdown(source)
			if err != nil || strings.Contains(string(rendered), " download ") == hidden || !strings.Contains(string(rendered), ">Open PDF</a>") || !strings.Contains(string(rendered), "<iframe") {
				t.Fatal("incorrect PDF actions", rendered, err)
			}
			refs, err := contentMediaReferences(source, "/post/s/pdf", "")
			if err != nil || !slices.Equal(refs, []string{"/files/" + file.ID}) {
				t.Fatal("Open PDF must retain the canonical file reference", refs, err)
			}
			feed, err := markdownAt(source, &url.URL{Scheme: "https", Host: "blog.example", Path: "/post/s/pdf"})
			if err != nil || strings.Contains(string(feed), ">Download</a>") == hidden || !strings.Contains(string(feed), ">Open PDF</a>") {
				t.Fatal("feed discarded the download link setting", feed, err)
			}
			// The setting only affects PDF previews, even when the same file is also a download.
			source, err = joinPostComponents("Body", componentInput{"downloads": downloadSource(t, file)})
			if err != nil {
				t.Fatal(err)
			}
			rendered, err = markdown(source)
			if err != nil || !strings.Contains(string(rendered), " download ") {
				t.Fatal("file downloads must remain downloadable", rendered, err)
			}
		})
	}
}

func TestPDFFormsHistoryAndAccess(t *testing.T) {
	for _, hx := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "htmx"}[hx], func(t *testing.T) {
			h := newHarness(t)
			h.register(t)
			h.verify(t)
			h.login(t)
			file := testPDF(t, h)
			file.HideDownload = true
			path := "/files/" + file.ID + "?preview=1"
			public := func(method string, headers map[string]string) *httptest.ResponseRecorder {
				r := httptest.NewRequest(method, "https://blog.example"+path, nil)
				for key, value := range headers {
					r.Header.Set(key, value)
				}
				w := httptest.NewRecorder()
				h.app.Handler.ServeHTTP(w, r)
				return w
			}
			private := func() {
				t.Helper()
				for _, method := range []string{"GET", "HEAD"} {
					for _, headers := range []map[string]string{nil, {"Range": "bytes=0-3"}, {"If-None-Match": "*"}} {
						w := public(method, headers)
						requireStatus(t, w, http.StatusNotFound)
						if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Frame-Options") != "DENY" {
							t.Fatal("private preview changed cache or framing policy", w.Header())
						}
					}
				}
			}
			private()
			checkPreview := func(w *httptest.ResponseRecorder) {
				t.Helper()
				requireStatus(t, w, http.StatusOK)
				kind, _, err := mime.ParseMediaType(w.Header().Get("Content-Disposition"))
				if err != nil || kind != "inline" || w.Header().Get("Content-Type") != "application/pdf" || w.Header().Get("X-Frame-Options") != "SAMEORIGIN" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'self'") {
					t.Fatal("PDF inline response", w.Header(), err)
				}
			}
			checkPreview(h.request("GET", path, nil, false))
			fields := url.Values{"title": {"PDF review"}, "slug": {"pdf-review"}, "pdf": {downloadSource(t, file)}, "componentOrder": {`["pdf-preview"]`}}
			fields.Set("title", "")
			errorPage := h.request("POST", "/post", fields, hx)
			requireStatus(t, errorPage, http.StatusUnprocessableEntity)
			if !hx && !strings.Contains(html.UnescapeString(errorPage.Body.String()), file.ID) {
				t.Fatal("ordinary validation discarded PDF")
			}
			fields.Set("title", "PDF review")
			for _, invalid := range []postDownload{
				{ID: file.ID, Label: file.Label, Size: file.Size + 1},
				{ID: strings.Repeat("a", 64) + ".pdf", Label: "Missing PDF", Size: file.Size},
			} {
				fields.Set("pdf", downloadSource(t, invalid))
				rejected := h.request("POST", "/post", fields, hx)
				requireStatus(t, rejected, http.StatusUnprocessableEntity)
				if !strings.Contains(rejected.Body.String(), `data-error-fields="pdf"`) {
					t.Fatal("invalid PDF must identify its component field")
				}
			}
			fields.Set("pdf", downloadSource(t, file))
			status := http.StatusSeeOther
			if hx {
				status = http.StatusOK
			}
			requireStatus(t, h.request("POST", "/post", fields, hx), status)
			post, err := h.blog.posts.GetBySlug(t.Context(), "pdf-review", true)
			if err != nil {
				t.Fatal(err)
			}
			versions, err := h.blog.history.ListRevisions(t.Context(), "post", post.ID)
			if err != nil || len(versions) != 1 {
				t.Fatal("missing PDF revision", err)
			}
			original, err := h.blog.history.GetRevision(t.Context(), "post", post.ID, versions[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			private()
			// Scheduled publication keeps the preview private until the publication time.
			post.Draft, post.Published = false, time.Now().Add(time.Hour)
			post, err = savePostFixture(h.blog.posts, t.Context(), post)
			if err != nil {
				t.Fatal(err)
			}
			private()
			post, err = setPostPublication(h.blog.posts, t.Context(), post.ID, false)
			if err != nil {
				t.Fatal(err)
			}
			post, err = setPostPublication(h.blog.posts, t.Context(), post.ID, true)
			if err != nil {
				t.Fatal(err)
			}
			response := public("GET", nil)
			checkPreview(response)
			requireStatus(t, public("HEAD", nil), http.StatusOK)
			requireStatus(t, public("GET", map[string]string{"If-None-Match": response.Header().Get("ETag")}), http.StatusNotModified)
			rangeResponse := public("GET", map[string]string{"Range": "bytes=0-3"})
			requireStatus(t, rangeResponse, http.StatusPartialContent)
			if rangeResponse.Body.String() != "%PDF" {
				t.Fatal("PDF ranges unavailable")
			}
			reader := h.request("GET", "/post/s/"+post.Slug, nil, false)
			if reader.Header().Get("X-Frame-Options") != "DENY" || !strings.Contains(reader.Header().Get("Content-Security-Policy"), "frame-src 'self'") || !strings.Contains(reader.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
				t.Fatal("ordinary pages must retain framing restrictions")
			}
			attachment := h.request("GET", "/files/"+file.ID, nil, false)
			if !strings.HasPrefix(attachment.Header().Get("Content-Disposition"), "attachment;") || attachment.Header().Get("X-Frame-Options") != "DENY" {
				t.Fatal("downloads must remain attachments")
			}
			post, err = setPostPublication(h.blog.posts, t.Context(), post.ID, false)
			if err != nil {
				t.Fatal(err)
			}
			private()
			post.Markdown = "PDF removed"
			post, err = savePostFixture(h.blog.posts, t.Context(), post)
			if err != nil {
				t.Fatal(err)
			}
			usage, err := h.blog.mediaReferences.ReferencedMedia(t.Context(), file.ID)
			if err != nil || usage[file.ID].Current || len(usage[file.ID].History) == 0 {
				t.Fatal("historical PDF retention", usage, err)
			}
			if err := h.blog.restoreRevisionContent(adminWritingContext(t.Context()), "post", post.ID, post.Version, original); err != nil {
				t.Fatal(err)
			}
			restored, err := h.blog.posts.GetByID(t.Context(), post.ID, true)
			if err != nil || restored.Markdown != original.Markdown {
				t.Fatal("PDF restoration", err)
			}
			private()
			text := testDownload(t, h, "notes.txt", "Not a PDF")
			requireStatus(t, h.request("GET", "/files/"+text.ID+"?preview=1", nil, false), http.StatusNotFound)
		})
	}
}
