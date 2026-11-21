package blog

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func relatedSource(t *testing.T, value postRelated) string {
	t.Helper()
	source, err := joinPostComponents("Body", componentInput{"related": componentJSON(value), "componentOrder": `["related-posts"]`})
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func TestRepeatedRelatedPosts(t *testing.T) {
	h := registeredBrowserHarness(t)
	ctx := t.Context()
	var ids []string
	for i := range maxRelatedLinks + 1 {
		number := strconv.Itoa(i)
		post, err := savePostFixture(h.blog.posts, ctx, Post{Slug: "other-post-" + number, Title: "Other post " + number, Markdown: "Body"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, post.ID)
	}
	extra := "related-posts:" + strings.Repeat("1", 32)
	source, err := joinPostComponents("Body", componentInput{
		"related":        componentJSON(postRelated{Title: "First group", Posts: ids[:maxRelatedLinks]}),
		extra:            componentJSON(postRelated{Title: "Second group", Posts: ids[maxRelatedLinks:]}),
		"componentOrder": componentJSON([]string{"related-posts", extra}),
	})
	if err != nil {
		t.Fatal(err)
	}
	output, err := h.blog.renderWriting(ctx, source, "")
	first, second, found := strings.Cut(string(output), `aria-label="Second group"`)
	if err != nil || !found || !strings.Contains(first, `href="/post/s/other-post-0"`) ||
		strings.Contains(first, `href="/post/s/other-post-12"`) || !strings.Contains(second, `href="/post/s/other-post-12"`) ||
		strings.Contains(second, `href="/post/s/other-post-0"`) {
		t.Fatalf("related links must render in their own component: %s, %v", output, err)
	}
}

func TestRelatedComponent(t *testing.T) {
	id := strings.Repeat("A", 26)
	value := postRelated{Title: `More <script> & reading`, Posts: []string{id}}
	source := relatedSource(t, value)
	page := newComponentEditorForm(source, nil)
	if page.fields["related"] != componentJSON(value) || !strings.Contains(searchableWriting(source), value.Title) || strings.Contains(searchableWriting(source), id) {
		t.Fatal("component must preserve its content and index authored text only")
	}
	if refs, err := contentMediaReferences(source, "/post/s/example", ""); err != nil || len(refs) != 0 {
		t.Fatal("post links must not become media references", refs, err)
	}
	for _, invalid := range []string{"{}", "null", `{"posts":[]}`, `{"posts":["../bad"]}`, `{"posts":["` + id + `","` + id + `"]}`, `{"posts":["` + id + `"],"unknown":true}`, componentJSON(postRelated{Title: strings.Repeat("x", 241), Posts: value.Posts})} {
		if _, err := parseRelated(invalid); err == nil {
			t.Fatalf("accepted invalid component: %s", invalid)
		}
	}
	for _, invalid := range []postRelated{
		{Posts: []string{id}, Links: []string{relatedPagePrefix + id}},
		{Posts: []string{relatedPagePrefix + id}},
		{Links: []string{"post:" + id}},
		{Links: []string{relatedPagePrefix + id, relatedPagePrefix + id}},
	} {
		if _, err := parseRelated(componentJSON(invalid)); err == nil {
			t.Fatalf("accepted invalid related links: %+v", invalid)
		}
	}
	bounded := postRelated{}
	for i := range maxRelatedLinks + 1 {
		bounded.Posts = append(bounded.Posts, strings.Repeat(string(rune('A'+i)), 26))
	}
	if _, err := parseRelated(componentJSON(bounded)); err == nil {
		t.Fatal("accepted more than 12 posts")
	}
	bounded.Posts = bounded.Posts[:maxRelatedLinks]
	if _, err := parseRelated(componentJSON(bounded)); err != nil {
		t.Fatal("rejected the selection limit", err)
	}
	reader := relatedRenderer{links: map[string]relatedLink{id: {title: `A <b>title</b>`, path: "/post/s/target"}}}
	rendered, err := markdownComponents(source, postComponentCatalog, reader)
	if err != nil || !strings.Contains(string(rendered), "A &lt;b&gt;title&lt;/b&gt;") || strings.Contains(string(rendered), "<script>") || strings.Contains(string(rendered), "<b>") {
		t.Fatal("unsafe link rendering", rendered, err)
	}
	if rendered, err := markdown(source); err != nil || strings.Contains(string(rendered), "related-posts") {
		t.Fatal("pure rendering must omit unresolved references", rendered, err)
	}
}

func TestRelatedPageLinksFollowPublication(t *testing.T) {
	h := registeredBrowserHarness(t)
	h.login(t)
	ctx := t.Context()
	save := func(page Page) Page {
		t.Helper()
		page.ChildrenDisplay = "hidden"
		updated, err := h.blog.persistPage(ctx, page)
		if err != nil {
			t.Fatal(err)
		}
		return updated
	}
	parent := save(Page{Title: "Parent", Published: true})
	child := save(Page{Title: "Related guide", ParentID: parent.ID, Published: true})
	post, err := h.blog.persistPost(ctx, Post{Title: "Related article", Markdown: "Body"})
	if err != nil {
		t.Fatal(err)
	}
	fields := pageFields("related-links-source", true)
	fields.Set("componentOrder", `["related-posts"]`)
	fields.Set("related", componentJSON(postRelated{Links: []string{relatedPagePrefix + child.ID, post.ID}}))
	requireStatus(t, h.request(http.MethodPost, "/pages", fields, true), http.StatusOK)
	source, err := h.blog.pageStore.GetPageBySlug(ctx, "related-links-source", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.blog.persistPost(ctx, Post{Title: "Post linking to a page", Markdown: source.Markdown}); err != nil {
		t.Fatal(err)
	}
	check := func(visible bool) {
		t.Helper()
		for _, path := range []string{source.URL(), "/post/s/post-linking-to-a-page", "/rss"} {
			w := h.request(http.MethodGet, path, nil, false)
			requireStatus(t, w, http.StatusOK)
			body := w.Body.String()
			base := ""
			if path == "/rss" {
				base = "https://blog.example"
			}
			if strings.Contains(body, base+child.URL()) != visible {
				t.Fatalf("incorrect visibility for %s in %s", child.URL(), path)
			}
			if path != "/rss" && visible && !strings.Contains(body, `<li><a href="`+child.URL()+`">`+child.Title+`</a></li><li><a href="/post/s/related-article">Related article</a></li>`) {
				t.Fatalf("related order or title changed on %s", path)
			}
		}
	}
	check(true)
	child.Title, child.Slug = "Renamed guide", "renamed-guide"
	child = save(child)
	check(true)
	if rss := h.request(http.MethodGet, "/rss", nil, false).Body.String(); strings.Contains(rss, "/page/related-guide") {
		t.Fatal("feed retained an old page slug")
	}
	parent.Published = false
	parent = save(parent)
	check(false)
	source.Summary = "Edit with a hidden link."
	source = save(source)
	parent.Published = true
	save(parent)
	check(true)
	requireStatus(t, h.request(http.MethodPost, "/pages/delete/"+child.ID, nil, false), http.StatusSeeOther)
	check(false)
	source.Summary = "Edit with a deleted link."
	source = save(source)
	if got := relatedReferences(source.Markdown); !slices.Equal(got, []string{relatedPagePrefix + child.ID, post.ID}) {
		t.Fatalf("page link changed after deletion: %v", got)
	}
	fields.Set("version", strconv.FormatInt(source.Version, 10))
	fields.Set("related", componentJSON(postRelated{Links: []string{relatedPagePrefix + source.ID}}))
	w := h.request(http.MethodPost, "/pages/"+source.ID, fields, true)
	requireStatus(t, w, http.StatusUnprocessableEntity)
	if !strings.Contains(w.Body.String(), `data-error-fields="related"`) {
		t.Fatal("self-link validation must identify the related component")
	}
}

func TestRelatedVisibilityAndCurrentLinks(t *testing.T) {
	h := registeredBrowserHarness(t)
	h.login(t)
	ctx := t.Context()
	first, err := h.blog.persistPost(ctx, Post{Title: "First target", Markdown: "Body"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.blog.persistPost(ctx, Post{Title: "Second target", Markdown: "Body"})
	if err != nil {
		t.Fatal(err)
	}
	source, err := h.blog.persistPost(ctx, Post{Title: "Related source", Markdown: relatedSource(t, postRelated{Posts: []string{second.ID, first.ID}})})
	if err != nil {
		t.Fatal(err)
	}
	read := func() string {
		t.Helper()
		w := h.request("GET", "/post/s/related-source", nil, false)
		requireStatus(t, w, http.StatusOK)
		return w.Body.String()
	}
	body := read()
	if strings.Index(body, `>Second target</a>`) < 0 || strings.Index(body, `>Second target</a>`) > strings.Index(body, `>First target</a>`) {
		t.Fatal("selected link order changed")
	}
	if strings.Contains(h.request("GET", "/post/s/first-target", nil, false).Body.String(), `class="related-posts"`) {
		t.Fatal("linking a target created a reciprocal component")
	}
	second.Title, second.Slug = "Renamed target", "renamed-target"
	second, err = h.blog.persistPost(ctx, second)
	if err != nil || !strings.Contains(read(), `href="/post/s/renamed-target">Renamed target</a>`) {
		t.Fatal("links did not follow the current title and slug", err)
	}
	feed := h.request("GET", "/rss", nil, false)
	requireStatus(t, feed, http.StatusOK)
	if !strings.Contains(feed.Body.String(), "https://blog.example/post/s/renamed-target") {
		t.Fatal("feed must resolve absolute related links")
	}
	second.Draft = true
	second, err = h.blog.persistPost(ctx, second)
	if err != nil || strings.Contains(read(), `>Renamed target</a>`) {
		t.Fatal("draft target remained visible", err)
	}
	if strings.Contains(h.request("GET", "/rss", nil, false).Body.String(), "renamed-target") {
		t.Fatal("cached feed retained a draft target")
	}
	source.Summary = "Source can still be edited."
	source, err = h.blog.persistPost(ctx, source)
	if err != nil || !slices.Contains(relatedReferences(source.Markdown), second.ID) {
		t.Fatal("editing source must preserve a hidden target", err)
	}
	second.Draft = false
	if _, err := h.blog.persistPost(ctx, second); err != nil || !strings.Contains(read(), `>Renamed target</a>`) {
		t.Fatal("republishing must restore the link", err)
	}
	for _, id := range []string{first.ID, second.ID} {
		if err := h.blog.posts.Delete(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	h.blog.invalidatePosts()
	if strings.Contains(read(), `class="related-posts"`) {
		t.Fatal("a component with no public targets must disappear")
	}
	source.Summary = "Deleted references remain stored."
	if saved, err := h.blog.persistPost(ctx, source); err != nil || len(relatedReferences(saved.Markdown)) != 2 {
		t.Fatal("deleted targets must not prevent source edits or lose references", err)
	}
}

func TestRelatedFormsAndHistory(t *testing.T) {
	for _, hx := range []bool{false, true} {
		t.Run(strconv.FormatBool(hx), func(t *testing.T) {
			h := registeredBrowserHarness(t)
			h.login(t)
			target, err := h.blog.persistPost(t.Context(), Post{Title: "A target", Markdown: "Body"})
			if err != nil {
				t.Fatal(err)
			}
			value := postRelated{Posts: []string{target.ID}}
			fields := url.Values{"title": {"Related form"}, "slug": {"related-form"}, "related": {componentJSON(value)}, "componentOrder": {`["related-posts"]`}}
			status := http.StatusSeeOther
			if hx {
				status = http.StatusOK
			}
			requireStatus(t, h.request("POST", "/post", fields, hx), status)
			post, err := h.blog.posts.GetBySlug(t.Context(), "related-form", true)
			if err != nil {
				t.Fatal(err)
			}
			versions, err := h.blog.history.ListRevisions(t.Context(), "post", post.ID)
			if err != nil || len(versions) != 1 {
				t.Fatal("missing component revision", err)
			}
			original, err := h.blog.history.GetRevision(t.Context(), "post", post.ID, versions[0].ID)
			if err != nil || !strings.Contains(original.Markdown, target.ID) {
				t.Fatal("revision lost related references", err)
			}
			fields.Set("version", strconv.FormatInt(post.Version, 10))
			for _, source := range []string{"", componentJSON(postRelated{Posts: []string{post.ID}}), componentJSON(postRelated{Posts: []string{strings.Repeat("Z", 26)}})} {
				fields.Set("related", source)
				w := h.request("POST", "/post/"+post.ID, fields, hx)
				requireStatus(t, w, http.StatusUnprocessableEntity)
				if !strings.Contains(w.Body.String(), `data-error-fields="related"`) {
					t.Fatal("validation did not identify the component")
				}
			}
			fields.Set("related", componentJSON(value))
			fields.Set("title", "")
			w := h.request("POST", "/post/"+post.ID, fields, hx)
			requireStatus(t, w, http.StatusUnprocessableEntity)
			if !hx && !strings.Contains(w.Body.String(), target.ID) {
				t.Fatal("ordinary validation discarded submitted links")
			}
			post.Markdown = relatedSource(t, postRelated{Title: "Changed heading", Posts: value.Posts})
			post, err = h.blog.persistPost(t.Context(), post)
			if err != nil {
				t.Fatal(err)
			}
			if err := h.blog.restoreRevisionContent(adminWritingContext(t.Context()), "post", post.ID, post.Version, original); err != nil {
				t.Fatal("unable to restore related content", err)
			}
			restored, err := h.blog.posts.GetByID(t.Context(), post.ID, true)
			if err != nil || restored.Markdown != original.Markdown {
				t.Fatal("restoration changed the component", err)
			}
		})
	}
}

func TestRelatedPrivateChoicesAndReferences(t *testing.T) {
	h := usersHarness(t)
	private, err := h.blog.persistPost(t.Context(), Post{Title: "Private admin target", Markdown: "Secret", Draft: true, AuthorID: "other"})
	if err != nil {
		t.Fatal(err)
	}
	adminCookies := h.cookies
	poster := registeredPoster(t, h)
	own, err := h.blog.persistPost(t.Context(), Post{Title: "My private target", Markdown: "Body", Draft: true, AuthorID: poster.ID})
	if err != nil {
		t.Fatal(err)
	}
	save := func(page Page) Page {
		t.Helper()
		page, err := savePageFixture(h.blog.pageStore, t.Context(), page)
		if err != nil {
			t.Fatal(err)
		}
		return page
	}
	parent := save(Page{Title: "Page choice parent", Slug: "private-parent"})
	child := save(Page{Title: "Page choice child", Slug: "private-child", ParentID: parent.ID, Published: true})
	public := save(Page{Title: "Page choice public", Slug: "public-page", Published: true})
	query := "/post/related?" + url.Values{"q": {"Page choice"}, "ids": {relatedPagePrefix + child.ID}}.Encode()
	response := h.request(http.MethodGet, query, nil, false)
	requireStatus(t, response, http.StatusOK)
	var result struct{ Choices, Selected []relatedChoice }
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || len(result.Selected) != 0 ||
		len(result.Choices) != 1 || result.Choices[0].ID != relatedPagePrefix+public.ID || strings.Contains(response.Body.String(), child.Title) {
		t.Fatalf("poster must see only public pages: %+v, %v", result, err)
	}
	fields := url.Values{"title": {"Link to a hidden page"}, "componentOrder": {`["related-posts"]`}, "related": {componentJSON(postRelated{Links: []string{relatedPagePrefix + child.ID}})}}
	requireStatus(t, h.request(http.MethodPost, "/post", fields, false), http.StatusUnprocessableEntity)
	w := h.request("GET", "/post/related?q=does-not-match&ids="+private.ID+","+own.ID, nil, false)
	requireStatus(t, w, http.StatusOK)
	var choices struct{ Choices, Selected []relatedChoice }
	if err := json.Unmarshal(w.Body.Bytes(), &choices); err != nil || len(choices.Selected) != 1 || choices.Selected[0].ID != own.ID || len(choices.Choices) != 0 || strings.Contains(w.Body.String(), private.Title) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("picker leaked private metadata or lost a selection during search", err)
	}
	for _, test := range []struct {
		id     string
		status int
	}{{private.ID, http.StatusUnprocessableEntity}, {own.ID, http.StatusSeeOther}} {
		fields := url.Values{"title": {"Related private review"}, "related": {componentJSON(postRelated{Posts: []string{test.id}})}, "componentOrder": {`["related-posts"]`}}
		requireStatus(t, h.request("POST", "/post", fields, false), test.status)
	}
	for _, query := range []string{"ids=bad", "exclude=bad", "q=" + strings.Repeat("x", 241)} {
		requireStatus(t, h.request("GET", "/post/related?"+query, nil, false), http.StatusBadRequest)
	}
	h.cookies = adminCookies
	response = h.request(http.MethodGet, query, nil, false)
	requireStatus(t, response, http.StatusOK)
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || len(result.Selected) != 1 ||
		result.Selected[0].State != "Hidden" || result.Selected[0].Kind != "Page" || len(result.Choices) != 2 || result.Choices[0].State != "Draft" {
		t.Fatalf("admin must see draft and hidden statuses: %+v, %v", result, err)
	}
	requireStatus(t, h.request(http.MethodPost, "/post", fields, false), http.StatusSeeOther)
	h.cookies = nil
	if w := h.request("GET", "/post/related", nil, false); w.Code == http.StatusOK || strings.Contains(w.Body.String(), own.Title) {
		t.Fatal("anonymous picker access exposed posts")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := h.blog.posts.PublicPostLinks(ctx, []string{own.ID}); !errors.Is(err, context.Canceled) {
		t.Fatal("lookup ignored cancellation", err)
	}
}

func TestRelatedSchedulingAndFailures(t *testing.T) {
	h := registeredBrowserHarness(t)
	start := time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)
	advance := publicationClock(h, start)
	target, err := h.blog.persistPost(t.Context(), Post{Title: "Later target", Markdown: "Body", Published: start.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	source := relatedSource(t, postRelated{Title: "Scheduled links", Posts: []string{target.ID}})
	if _, err := h.blog.posts.Save(t.Context(), Post{Title: "Source", Slug: "source", Markdown: source}, nil); err != nil {
		t.Fatal(err)
	}
	h.blog.invalidatePosts()
	for _, visible := range []bool{false, true} {
		if visible {
			advance(start.Add(time.Hour))
		}
		for _, path := range []string{"/post/s/source", "/rss"} {
			w := h.request("GET", path, nil, false)
			requireStatus(t, w, http.StatusOK)
			if strings.Contains(w.Body.String(), "Scheduled links") != visible {
				t.Fatalf("scheduled related links have incorrect visibility in %s", path)
			}
		}
	}
	original := h.blog.posts
	h.blog.posts = failedRelatedPosts{original}
	defer func() { h.blog.posts = original }()
	if _, err := h.blog.renderWriting(t.Context(), source, ""); err == nil {
		t.Fatal("renderer concealed a storage failure")
	}
	h.login(t)
	w := h.request("GET", "/post/related?q=target", nil, false)
	requireStatus(t, w, http.StatusInternalServerError)
}

type failedRelatedPosts struct{ repository }

func (failedRelatedPosts) PublicPostLinks(context.Context, []string) ([]Post, error) {
	return nil, errors.New("post lookup unavailable")
}
func (failedRelatedPosts) ListMetadata(context.Context) ([]Post, error) {
	return nil, errors.New("post choices unavailable")
}

func TestRelatedChoicesRequireSearch(t *testing.T) {
	h := registeredBrowserHarness(t)
	h.login(t)
	post, err := h.blog.persistPost(t.Context(), Post{Title: "Searchable target", Markdown: "Body"})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		query           string
		posts, selected int
	}{
		{"", 0, 0},
		{"?q=%20%20", 0, 0},
		{"?ids=" + post.ID, 0, 1},
		{"?q=Searchable", 1, 0},
	} {
		w := h.request("GET", "/post/related"+test.query, nil, false)
		requireStatus(t, w, http.StatusOK)
		var choices struct {
			Choices, Selected []relatedChoice
		}
		if err := json.Unmarshal(w.Body.Bytes(), &choices); err != nil || len(choices.Choices) != test.posts || len(choices.Selected) != test.selected {
			t.Fatalf("unexpected choices for %q: %+v, %v", test.query, choices, err)
		}
	}
}

func TestRelatedChoicesPrioritizeExactTitle(t *testing.T) {
	h := registeredBrowserHarness(t)
	h.login(t)
	for i := range 31 {
		if _, err := h.blog.persistPost(t.Context(), Post{Title: "A guide " + strconv.Itoa(i), Markdown: "Body"}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := savePageFixture(h.blog.pageStore, t.Context(), Page{Title: "Guide", Slug: "guide", Published: true})
	if err != nil {
		t.Fatal(err)
	}
	w := h.request(http.MethodGet, "/post/related?q=GUIDE", nil, false)
	requireStatus(t, w, http.StatusOK)
	var result struct{ Choices []relatedChoice }
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || len(result.Choices) != 30 || result.Choices[0].ID != relatedPagePrefix+page.ID {
		t.Fatalf("the exact page title must lead the capped matches: %+v, %v", result, err)
	}
}

func TestRelatedSelectionResolvesOnlySelectedKinds(t *testing.T) {
	h := registeredBrowserHarness(t)
	h.login(t)
	post, err := h.blog.persistPost(t.Context(), Post{Title: "Related target", Markdown: "Body"})
	if err != nil {
		t.Fatal(err)
	}
	page, err := savePageFixture(h.blog.pageStore, t.Context(), Page{Title: "Page target", Slug: "page-target", Published: true})
	if err != nil {
		t.Fatal(err)
	}
	check := func(id string) {
		t.Helper()
		w := h.request(http.MethodGet, "/post/related?"+url.Values{"ids": {id}}.Encode(), nil, false)
		requireStatus(t, w, http.StatusOK)
		var result struct{ Selected []relatedChoice }
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || len(result.Selected) != 1 || result.Selected[0].ID != id {
			t.Fatalf("selection must resolve independently: %+v, %v", result, err)
		}
	}
	pageStore := h.blog.pageStore
	h.blog.pageStore = unavailablePages{}
	check(post.ID)
	h.blog.pageStore = pageStore
	h.blog.posts = failedRelatedPosts{h.blog.posts}
	check(relatedPagePrefix + page.ID)
}
