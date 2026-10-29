package blog

import (
	"context"
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// sharingTags reads metadata as a crawler would, including HTML entity decoding.
func sharingTags(t *testing.T, source string) map[string]string {
	t.Helper()
	return metadataTags(t, source, "property", "og:")
}

// metadataTags reads selected meta tags and rejects duplicate keys.
func metadataTags(t *testing.T, source, attribute, prefix string) map[string]string {
	t.Helper()
	tags := make(map[string]string)
	tokens := html.NewTokenizer(strings.NewReader(source))
	for {
		switch tokens.Next() {
		case html.ErrorToken:
			return tags
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokens.Token()
			if token.Data != "meta" {
				continue
			}
			var property, value string
			for _, attr := range token.Attr {
				if attr.Key == attribute {
					property = attr.Val
				}
				if attr.Key == "content" {
					value = attr.Val
				}
			}
			if strings.HasPrefix(property, prefix) {
				if _, exists := tags[property]; exists {
					t.Fatal("duplicate metadata", property)
				}
				tags[property] = value
			}
		}
	}
}

// requireDescription checks the ordinary description independently of Open Graph tags.
func requireDescription(t *testing.T, source, want string) {
	t.Helper()
	tags := metadataTags(t, source, "name", "description")
	got, exists := tags["description"]
	if !exists || got != want {
		t.Fatalf("description=%q (present=%v), want %q", got, exists, want)
	}
}

// requireCanonical checks for exactly one canonical link in the head, or none
// when want is empty. It also rejects empty and duplicate canonical links.
func requireCanonical(t *testing.T, source, want string) {
	t.Helper()
	tokens := html.NewTokenizer(strings.NewReader(source))
	var found, inHead bool
	for {
		switch tokens.Next() {
		case html.ErrorToken:
			if found != (want != "") {
				t.Fatalf("canonical present=%v, want URL %q", found, want)
			}
			return
		case html.EndTagToken:
			if tokens.Token().Data == "head" {
				inHead = false
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokens.Token()
			if token.Data == "head" {
				inHead = true
			}
			if token.Data != "link" {
				continue
			}
			var rel, href string
			for _, attr := range token.Attr {
				switch attr.Key {
				case "rel":
					rel = attr.Val
				case "href":
					href = attr.Val
				}
			}
			if rel == "canonical" {
				if found || !inHead || href == "" || href != want {
					t.Fatalf("unexpected canonical: href=%q, duplicate=%v, inHead=%v, want=%q", href, found, inHead, want)
				}
				found = true
			}
		}
	}
}

// TestSharingPublicContent covers full-page metadata, stable URLs, image priority,
// canonical URLs, description agreement, escaping, and updates.
func TestSharingPublicContent(t *testing.T) {
	h := newHarness(t)
	profile := Profile{Name: `A "site" & friends`, Bio: "Site biography", ImageURL: "/local-assets/portrait.jpg"}
	if err := h.blog.writeProfile(t.Context(), profile); err != nil {
		t.Fatal(err)
	}
	post, err := h.blog.posts.Save(t.Context(), Post{Slug: "shared", Title: `Title " /><script>alert(1)</script>`, Summary: `Summary " /><script>alert(1)</script> & details`, Markdown: "![Vector](/vector.svg)\n\n![Photo](/images/photo.png?size=large&crop=1)"})
	if err != nil {
		t.Fatal(err)
	}
	imageID, err := h.blog.images.Put(t.Context(), testImageUpload("image/png", testPNG(t)))
	if err != nil {
		t.Fatal(err)
	}
	p, err := h.blog.pageStore.SavePage(t.Context(), Page{Slug: "featured", Title: "Featured", Published: true, Summary: "Page summary", PreviewImage: imageID, Markdown: "![Other](/other.jpg)"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path, title, description, kind, target, image string
	}{
		{"/", profile.Name, profile.Bio, "website", "/", "/local-assets/portrait.jpg"},
		{"/?tracking=1", profile.Name, profile.Bio, "website", "/", "/local-assets/portrait.jpg"},
		{"/posts", "All Posts", profile.Bio, "website", "/posts", "/local-assets/portrait.jpg"},
		{"/posts?tracking=1", "All Posts", profile.Bio, "website", "/posts", "/local-assets/portrait.jpg"},
		{"/post/s/shared", post.Title, post.Summary, "article", "/post/s/shared", "/images/photo.png?size=large&crop=1"},
		{"/post/s/shared?tracking=1", post.Title, post.Summary, "article", "/post/s/shared", "/images/photo.png?size=large&crop=1"},
		{"/post/id/" + post.ID, post.Title, post.Summary, "article", "/post/s/shared", "/images/photo.png?size=large&crop=1"},
		{p.URL(), p.Title, p.Summary, "website", p.URL(), "/images/" + imageID},
		{p.URL() + "?tracking=1", p.Title, p.Summary, "website", p.URL(), "/images/" + imageID},
		{"/project/s/featured", p.Title, p.Summary, "website", p.URL(), "/images/" + imageID},
		{"/page/about", "About Me", profile.Bio, "website", "/about", "/local-assets/portrait.jpg"},
		{"/projects", "Projects", profile.Bio, "website", "/projects", "/local-assets/portrait.jpg"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			response := h.request("GET", tc.path, nil, false)
			requireStatus(t, response, http.StatusOK)
			requireCanonical(t, response.Body.String(), "https://blog.example"+tc.target)
			tags := sharingTags(t, response.Body.String())
			for key, want := range map[string]string{"og:site_name": profile.Name, "og:title": tc.title, "og:description": tc.description, "og:type": tc.kind, "og:url": "https://blog.example" + tc.target, "og:image": "https://blog.example" + tc.image} {
				if tags[key] != want {
					t.Errorf("%s=%q, want %q", key, tags[key], want)
				}
			}
			requireDescription(t, response.Body.String(), tc.description)
			if strings.Contains(response.Body.String(), "<script>alert(1)</script>") {
				t.Fatal("metadata escaped incorrectly")
			}
			fragment := h.request("GET", tc.path, nil, true).Body.String()
			requireCanonical(t, fragment, "")
			if tags := sharingTags(t, fragment); len(tags) != 0 {
				t.Fatal("fragment contains sharing metadata", tags)
			}
			if tags := metadataTags(t, fragment, "name", "description"); len(tags) != 0 {
				t.Fatal("fragment contains a description tag", tags)
			}
		})
	}
	post.Summary, post.Markdown = " ", "No images"
	if post, err = h.blog.posts.Save(t.Context(), post); err != nil {
		t.Fatal(err)
	}
	profile.Bio, profile.ImageURL = "Updated bio", "https://cdn.example/new.jpg"
	if err := h.blog.writeProfile(t.Context(), profile); err != nil {
		t.Fatal(err)
	}
	body := h.request("GET", "/post/s/shared", nil, false).Body.String()
	requireDescription(t, body, profile.Bio)
	tags := sharingTags(t, body)
	if tags["og:description"] != profile.Bio || tags["og:image"] != profile.ImageURL {
		t.Fatal("content or profile changes left stale metadata", tags)
	}
	p.PreviewImage = ""
	if _, err := h.blog.pageStore.SavePage(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	if tags := sharingTags(t, h.request("GET", p.URL(), nil, false).Body.String()); tags["og:image"] != "https://blog.example/other.jpg" {
		t.Fatal("page without a preview ignored its content image", tags)
	}
}

// TestSharingVisibility prevents public metadata on private content, forms, and errors.
func TestSharingVisibility(t *testing.T) {
	h := newHarness(t)
	post, err := h.blog.posts.Save(t.Context(), Post{Slug: "private-post", Title: "Private post", Summary: "Private post summary", Markdown: "Body", Draft: true})
	if err != nil {
		t.Fatal(err)
	}
	parent, err := h.blog.pageStore.SavePage(t.Context(), Page{Slug: "private-page", Title: "Private page", Summary: "Private page summary"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := h.blog.pageStore.SavePage(t.Context(), Page{Slug: "hidden-child", Title: "Hidden child", Summary: "Hidden child summary", ParentID: parent.ID, Published: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, admin := range []bool{false, true} {
		if admin {
			h.register(t)
			h.verify(t)
			h.login(t)
		}
		for _, path := range []string{"/post/s/private-post", parent.URL(), child.URL(), "/missing", "/post/s/missing", "/auth/login/email", "/auth/register/email", "/auth/reset/request/email", "/post/manage", "/pages", "/profile", "/post/create", "/post/update/" + post.ID, "/pages/update/" + child.ID} {
			response := h.request("GET", path, nil, false)
			requireCanonical(t, response.Body.String(), "")
			if tags := sharingTags(t, response.Body.String()); len(tags) != 0 {
				t.Fatalf("admin=%v path=%s exposed sharing metadata: %v", admin, path, tags)
			}
			if tags := metadataTags(t, response.Body.String(), "name", "description"); len(tags) != 0 && tags["description"] != h.blog.profile.Current().Bio {
				t.Fatalf("admin=%v path=%s exposed a private description: %v", admin, path, tags)
			}
		}
	}
}

// TestCanonicalURLsFollowSlugChanges uses current slugs, including for a signed-in
// administrator viewing public content through the stable post-ID route.
func TestCanonicalURLsFollowSlugChanges(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	post, err := h.blog.posts.Save(t.Context(), Post{Slug: "original", Title: "Post"})
	if err != nil {
		t.Fatal(err)
	}
	page, err := h.blog.pageStore.SavePage(t.Context(), Page{Slug: "original", Title: "Page", Published: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{"original", "renamed"} {
		post.Slug, page.Slug = slug, slug
		if post, err = h.blog.posts.Save(t.Context(), post); err != nil {
			t.Fatal(err)
		}
		if page, err = h.blog.pageStore.SavePage(t.Context(), page); err != nil {
			t.Fatal(err)
		}
		for path, canonical := range map[string]string{
			"/post/id/" + post.ID: "https://blog.example/post/s/" + slug,
			page.URL():            "https://blog.example" + page.URL(),
		} {
			response := h.request("GET", path, nil, false)
			requireStatus(t, response, http.StatusOK)
			requireCanonical(t, response.Body.String(), canonical)
		}
	}
}

// TestPageDescriptions checks summary edits, blank fallbacks, and independence
// from the public URL needed by Open Graph metadata.
func TestPageDescriptions(t *testing.T) {
	h := newHarness(t)
	post, err := h.blog.posts.Save(t.Context(), Post{Slug: "description", Title: "Post"})
	if err != nil {
		t.Fatal(err)
	}
	page, err := h.blog.pageStore.SavePage(t.Context(), Page{Slug: "description", Title: "Page", Published: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, summary := range []string{"", " \t\n", "Updated summary"} {
		post.Summary, page.Summary = summary, summary
		if post, err = h.blog.posts.Save(t.Context(), post); err != nil {
			t.Fatal(err)
		}
		if page, err = h.blog.pageStore.SavePage(t.Context(), page); err != nil {
			t.Fatal(err)
		}
		want := summary
		if strings.TrimSpace(want) == "" {
			want = h.blog.profile.Current().Bio
		}
		for _, path := range []string{"/post/s/description", page.URL()} {
			response := h.request("GET", path, nil, false)
			requireStatus(t, response, http.StatusOK)
			requireDescription(t, response.Body.String(), want)
		}
	}

	for _, origin := range []string{"invalid origin", ""} {
		h.blog.app.Config.HTTP.PublicOrigin = origin
		h.blog.app.Config.HTTP.ListenHost = "0.0.0.0"
		for _, path := range []string{"/post/s/description", page.URL()} {
			response := h.request("GET", path, nil, false)
			requireStatus(t, response, http.StatusOK)
			requireDescription(t, response.Body.String(), "Updated summary")
			requireCanonical(t, response.Body.String(), "")
			if tags := sharingTags(t, response.Body.String()); len(tags) != 0 {
				t.Fatal("unavailable public URL advertised Open Graph metadata", tags)
			}
		}
	}
	if err := h.blog.writeProfile(t.Context(), Profile{Name: "My Blog"}); err != nil {
		t.Fatal(err)
	}
	requireDescription(t, h.request("GET", "/", nil, false).Body.String(), "")
}

// TestSharingOriginAndDefaults checks trusted URLs, neutral defaults, and failure isolation.
func TestSharingOriginAndDefaults(t *testing.T) {
	h := newHarness(t)
	if err := h.blog.writeProfile(t.Context(), Profile{Name: "My Blog"}); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "https://spoofed.example/?private=query", nil)
	r.Header.Set("Forwarded", "host=forwarded.example;proto=http")
	r.Header.Set("X-Forwarded-Host", "forwarded.example")
	w := httptest.NewRecorder()
	h.app.Handler.ServeHTTP(w, r)
	requireStatus(t, w, http.StatusOK)
	requireCanonical(t, w.Body.String(), "https://blog.example/")
	tags := sharingTags(t, w.Body.String())
	if tags["og:url"] != "https://blog.example/" || tags["og:title"] != "My Blog" || tags["og:image"] != "" || strings.Contains(tags["og:site_name"], "Trent") {
		t.Fatal("incorrect origin or neutral defaults", tags)
	}
	h.blog.app.Config.HTTP.PublicOrigin = "invalid origin"
	w = h.request("GET", "/about", nil, false)
	requireStatus(t, w, http.StatusOK)
	requireCanonical(t, w.Body.String(), "")
	if tags := sharingTags(t, w.Body.String()); len(tags) != 0 {
		t.Fatal("invalid public origin advertised metadata", tags)
	}
}

// TestSharingImageSelection covers unsafe URLs, SVG fallback, and relative resolution.
func TestSharingImageSelection(t *testing.T) {
	base, _ := url.Parse("https://blog.example/post/s/title")
	for _, tc := range []struct{ raw, want string }{
		{"/images/photo.png", "https://blog.example/images/photo.png"},
		{"photo.jpg", "https://blog.example/post/s/photo.jpg"},
		{"//cdn.example/photo.webp", "https://cdn.example/photo.webp"},
		{"https://cdn.example/photo?id=1&size=2#crop", "https://cdn.example/photo?id=1&size=2"},
		{"http://cdn.example/photo.gif", "http://cdn.example/photo.gif"},
		{"", ""}, {"#fragment", ""}, {"?query=only", ""}, {"/image.SVG?size=1", ""},
		{"javascript:alert(1)", ""}, {"data:image/png;base64,AA", ""}, {"file:///image.png", ""},
		{"https://user:secret@cdn.example/image.png", ""}, {"https://[invalid/image.png", ""},
	} {
		if got := sharingImageURL(tc.raw, base); got != tc.want {
			t.Errorf("sharingImageURL(%q)=%q, want %q", tc.raw, got, tc.want)
		}
	}
	rendered := template.HTML(`<img src="data:image/png;base64,AA"><img src="/vector.svg"><img src="/first.png?a=1&amp;b=2"><img src="/second.png">`)
	if got := firstSharingImage(rendered, base); got != "https://blog.example/first.png?a=1&b=2" {
		t.Fatal("wrong first suitable image", got)
	}
}

type sharingLookupFailure struct{ pageRepository }

func (s sharingLookupFailure) GetPageByID(ctx context.Context, id string, includeDrafts bool) (Page, error) {
	if !includeDrafts {
		return Page{}, errors.New("public visibility lookup failed")
	}
	return s.pageRepository.GetPageByID(ctx, id, includeDrafts)
}

// TestSharingVisibilityFailure keeps metadata absent when visibility is uncertain.
func TestSharingVisibilityFailure(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	h.blog.pageStore = sharingLookupFailure{h.blog.pageStore}
	w := h.request("GET", "/about", nil, false)
	requireStatus(t, w, http.StatusOK)
	requireCanonical(t, w.Body.String(), "")
	if tags := sharingTags(t, w.Body.String()); len(tags) != 0 {
		t.Fatal("failed visibility check advertised metadata", tags)
	}
}
