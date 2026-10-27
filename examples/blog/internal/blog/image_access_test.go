package blog

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// TestImageAccessLifecycle exercises access before HTTP conditional processing,
// including drafts, publication, shared references, deletion, and administrator access.
func TestImageAccessLifecycle(t *testing.T) {
	h := newHarness(t)
	id, err := h.blog.images.Put(t.Context(), "image/png", testPNG(t))
	if err != nil {
		t.Fatal(err)
	}
	request := func(method string, headers map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/images/"+id, nil)
		for key, value := range headers {
			r.Header.Set(key, value)
		}
		w := httptest.NewRecorder()
		h.app.Handler.ServeHTTP(w, r)
		return w
	}
	checkPrivate := func() {
		t.Helper()
		for _, method := range []string{"GET", "HEAD"} {
			for _, headers := range []map[string]string{nil, {"If-None-Match": "*"}, {"Range": "bytes=0-7"}, {"If-Modified-Since": "Wed, 21 Oct 2099 07:28:00 GMT"}} {
				w := request(method, headers)
				requireStatus(t, w, http.StatusNotFound)
				if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("ETag") != "" || bytes.Equal(w.Body.Bytes(), testPNG(t)) {
					t.Fatal("private response exposed cache validators or image bytes", w.Header())
				}
			}
		}
	}
	checkPrivate() // Upload alone grants no public access.
	h.register(t)
	h.login(t)
	requireStatus(t, h.request("GET", "/images/"+id, nil, false), http.StatusNotFound)
	h.verify(t)
	admin := h.request("GET", "/images/"+id, nil, false)
	requireStatus(t, admin, http.StatusOK)
	if admin.Header().Get("Cache-Control") != "no-store" || !bytes.Equal(admin.Body.Bytes(), testPNG(t)) {
		t.Fatal("administrator preview was missing or cacheable")
	}
	p, err := h.blog.posts.Save(t.Context(), Post{Slug: "draft-image", Title: "Draft", Draft: true, Markdown: "![Draft](/images/" + id + ")"})
	if err != nil {
		t.Fatal(err)
	}
	checkPrivate()
	if p, err = h.blog.posts.SetPublished(t.Context(), p.ID, true); err != nil {
		t.Fatal(err)
	}
	public := request("GET", nil)
	requireStatus(t, public, http.StatusOK)
	if public.Header().Get("Cache-Control") != "public, no-cache" {
		t.Fatal("public response does not require revalidation")
	}
	etag := public.Header().Get("ETag")
	requireStatus(t, request("GET", map[string]string{"If-None-Match": etag}), http.StatusNotModified)
	if p, err = h.blog.posts.SetPublished(t.Context(), p.ID, false); err != nil {
		t.Fatal(err)
	}
	checkPrivate()
	requireStatus(t, request("GET", map[string]string{"If-None-Match": etag, "Range": "bytes=0-7"}), http.StatusNotFound)
	shared, err := h.blog.posts.Save(t.Context(), Post{Slug: "shared-image", Title: "Shared", PreviewImage: id})
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request("GET", nil), http.StatusOK)
	if err := h.blog.posts.Delete(t.Context(), shared.ID); err != nil {
		t.Fatal(err)
	}
	checkPrivate()
	// Editing the last public reference revokes access without deleting the file.
	p.Draft, p.PreviewImage, p.Markdown = false, id, ""
	if p, err = h.blog.posts.Save(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request("GET", nil), http.StatusOK)
	p.PreviewImage = ""
	if p, err = h.blog.posts.Save(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	checkPrivate()
	requireStatus(t, h.request("GET", "/images/"+id, nil, false), http.StatusOK)
}

// TestPageImageAccess follows the complete ancestry and profile's current URLs.
func TestPageImageAccess(t *testing.T) {
	h := newHarness(t)
	id, err := h.blog.images.Put(t.Context(), "image/png", testPNG(t))
	if err != nil {
		t.Fatal(err)
	}
	check := func(status int) {
		t.Helper()
		requireStatus(t, h.request("GET", "/images/"+id, nil, false), status)
	}
	parent, err := h.blog.pageStore.SavePage(t.Context(), Page{Slug: "parent", Title: "Parent"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := h.blog.pageStore.SavePage(t.Context(), Page{Slug: "child", Title: "Child", ParentID: parent.ID, Published: true, PreviewImage: id})
	if err != nil {
		t.Fatal(err)
	}
	check(http.StatusNotFound)
	parent.Published = true
	if parent, err = h.blog.pageStore.SavePage(t.Context(), parent); err != nil {
		t.Fatal(err)
	}
	check(http.StatusOK)
	parent.Published = false
	if parent, err = h.blog.pageStore.SavePage(t.Context(), parent); err != nil {
		t.Fatal(err)
	}
	check(http.StatusNotFound)
	child.ParentID, child.PreviewImage, child.Markdown = "", "", "![Image](../images/"+id+")"
	if _, err := h.blog.pageStore.SavePage(t.Context(), child); err != nil {
		t.Fatal(err)
	}
	check(http.StatusOK)
	if err := h.blog.pageStore.DeletePage(t.Context(), child.ID); err != nil {
		t.Fatal(err)
	}
	check(http.StatusNotFound)
	for _, field := range []string{"portrait", "favicon", "background"} {
		p := Profile{Name: "Profile"}
		switch field {
		case "portrait":
			p.ImageURL = "/images/" + id
		case "favicon":
			p.FaviconURL = "https://blog.example/images/" + id + "?v=1"
		case "background":
			p.BackgroundURL = "/images/" + id + "#crop"
		}
		if err := h.blog.writeProfile(t.Context(), p); err != nil {
			t.Fatal(err)
		}
		check(http.StatusOK)
		if err := h.blog.writeProfile(t.Context(), Profile{Name: "Profile"}); err != nil {
			t.Fatal(err)
		}
		check(http.StatusNotFound)
	}
}

// TestImageReferenceParsing grants access for rendered references only. Escaping,
// relative paths, query strings, and foreign hosts cannot confuse the lookup.
func TestImageReferenceParsing(t *testing.T) {
	for _, tc := range []struct {
		source, path, want string
	}{
		{"![Image](/images/a.png)", "/post/s/title", "/images/a.png"},
		{"[Download](../../images/a.png?size=2#part)", "/post/s/title", "/images/a.png"},
		{"![Image](/images/%61.png)", "/page/example", "/images/a.png"},
		{"![Image](/images/&#97;.png)", "/page/example", "/images/a.png"},
		{"![Image][pic]\n\n[pic]: /images/a.png", "/", "/images/a.png"},
		{"![Image](https://other.example/images/a.png)", "/", "//other.example/images/a.png"},
		{"![Image](//blog.example/images/a.png)", "/", "//blog.example/images/a.png"},
		{"![Image](https://BLOG.example:443/images/a.png)", "/", "//blog.example/images/a.png"},
		{"`![Image](/images/a.png)`", "/", ""},
		{"```\n![Image](/images/a.png)\n```", "/", ""},
		{"[unused]: /images/a.png", "/", ""},
		{"<!-- ![Image](/images/a.png) -->", "/", ""},
		{"<img src='/images/a.png'>", "/", ""},
		{"/images/a.png", "/", ""},
		{"![Image](https://user:password@blog.example/images/a.png)", "/", ""},
		{"![Image](https:/images/a.png)", "/", ""},
		{"![Image](http:///images/a.png)", "/", ""},
	} {
		refs, err := imageReferences(tc.source, tc.path, "")
		if err != nil || (tc.want == "" && len(refs) != 0) || (tc.want != "" && !slices.Equal(refs, []string{tc.want})) {
			t.Errorf("%q: refs=%v error=%v, want %q", tc.source, refs, err, tc.want)
		}
	}
	h := newHarness(t)
	id, err := h.blog.images.Put(t.Context(), "image/png", testPNG(t))
	if err != nil {
		t.Fatal(err)
	}
	p, err := h.blog.posts.Save(t.Context(), Post{Slug: "external", Title: "External", Markdown: "![Image](https://other.example/images/" + id + ")"})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "https://other.example/images/"+id, nil)
	r.Header.Set("X-Forwarded-Host", "other.example")
	w := httptest.NewRecorder()
	h.app.Handler.ServeHTTP(w, r)
	requireStatus(t, w, http.StatusNotFound)
	p.Markdown = "![Image](https://blog.example/images/" + id + ")"
	if p, err = h.blog.posts.Save(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, h.request("GET", "/images/"+id, nil, false), http.StatusOK)
	// A wildcard listener without a public origin still supports root-relative
	// uploads, but must not trust a request host to authorize absolute references.
	h.blog.app.Config.HTTP.PublicOrigin = ""
	h.blog.app.Config.HTTP.ListenHost = "0.0.0.0"
	requireStatus(t, h.request("GET", "/images/"+id, nil, false), http.StatusNotFound)
	p.Markdown = "![Image](/images/" + id + ")"
	if p, err = h.blog.posts.Save(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, h.request("GET", "/images/"+id, nil, false), http.StatusOK)
}

// TestImageReferenceDefaultPorts checks trusted URL aliases across public content
// and profile images. A different host or nondefault port must not grant access.
func TestImageReferenceDefaultPorts(t *testing.T) {
	for _, tc := range []struct {
		origin, authority string
		public            bool
	}{
		{"https://blog.example", "//blog.example:443", true},
		{"https://blog.example:443", "//BLOG.example:443", true},
		{"https://blog.example:443", "//blog.example", true},
		{"http://blog.example", "//blog.example:80", true},
		{"http://blog.example:80", "//blog.example:80", true},
		{"https://blog.example", "//blog.example:80", false},
		{"http://blog.example", "//blog.example:443", false},
		{"https://blog.example", "//blog.example:8443", false},
		{"https://blog.example", "//other.example:443", false},
		{"https://blog.example:8443", "//blog.example:443", false},
		{"https://blog.example:8443", "//blog.example", false},
		{"https://blog.example:8443", "//blog.example:8443", true},
	} {
		t.Run(tc.origin+tc.authority, func(t *testing.T) {
			h := newHarness(t)
			id, err := h.blog.images.Put(t.Context(), "image/png", testPNG(t))
			if err != nil {
				t.Fatal(err)
			}
			h.blog.app.Config.HTTP.PublicOrigin = tc.origin
			ref := tc.authority + "/images/" + id
			status := http.StatusNotFound
			if tc.public {
				status = http.StatusOK
			}
			post, err := h.blog.posts.Save(t.Context(), Post{Slug: "port-test", Title: "Port test", Markdown: "![Image](" + ref + ")"})
			if err != nil {
				t.Fatal(err)
			}
			requireStatus(t, h.request("GET", "/images/"+id, nil, false), status)
			if err := h.blog.posts.Delete(t.Context(), post.ID); err != nil {
				t.Fatal(err)
			}
			page, err := h.blog.pageStore.SavePage(t.Context(), Page{Slug: "port-test", Title: "Port test", Published: true, Markdown: "![Image](" + ref + ")"})
			if err != nil {
				t.Fatal(err)
			}
			requireStatus(t, h.request("GET", "/images/"+id, nil, false), status)
			if err := h.blog.pageStore.DeletePage(t.Context(), page.ID); err != nil {
				t.Fatal(err)
			}
			profile := h.blog.profile.Current()
			for _, field := range []*string{&profile.ImageURL, &profile.FaviconURL, &profile.BackgroundURL} {
				*field = ref
				if err := h.blog.writeProfile(t.Context(), profile); err != nil {
					t.Fatal(err)
				}
				requireStatus(t, h.request("GET", "/images/"+id, nil, false), status)
				*field = ""
			}
			if err := h.blog.writeProfile(t.Context(), profile); err != nil {
				t.Fatal(err)
			}
			requireStatus(t, h.request("GET", "/images/"+id, nil, false), http.StatusNotFound)
		})
	}
}

// TestProfileImageIPv6DefaultPort keeps IPv6 brackets when building URL aliases.
func TestProfileImageIPv6DefaultPort(t *testing.T) {
	h := newHarness(t)
	id, err := h.blog.images.Put(t.Context(), "image/png", testPNG(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"https://[::1]", "https://[::1]:443", "http://[::1]"} {
		h.blog.app.Config.HTTP.PublicOrigin = origin
		port := "443"
		if strings.HasPrefix(origin, "http:") {
			port = "80"
		}
		profile := Profile{Name: "Profile", ImageURL: "//[::1]:" + port + "/images/" + id}
		if err := h.blog.writeProfile(t.Context(), profile); err != nil {
			t.Fatal(err)
		}
		requireStatus(t, h.request("GET", "/images/"+id, nil, false), http.StatusOK)
	}
}

type failedImageAccess struct{}

func (failedImageAccess) ImageIsPublic(context.Context, ...string) (bool, error) {
	return false, errors.New("private visibility lookup failure")
}

// TestImageAccessFailure fails closed without revealing storage details or validators.
func TestImageAccessFailure(t *testing.T) {
	h := newHarness(t)
	h.blog.imageAccess = failedImageAccess{}
	w := h.request("GET", "/images/"+strings.Repeat("a", 64)+".png", nil, false)
	requireStatus(t, w, http.StatusInternalServerError)
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("ETag") != "" || strings.Contains(w.Body.String(), "private visibility") {
		t.Fatal("visibility failure exposed details or cached data")
	}
}

// TestImageReferenceTransactions verifies that content and references roll back
// together and that deleting an owner removes its reference rows.
func TestImageReferenceTransactions(t *testing.T) {
	h := newHarness(t)
	store := h.blog.posts.(*sqliteRepository)
	for _, kind := range []string{"post", "page"} {
		t.Run(kind, func(t *testing.T) {
			var id string
			var version int64
			save := func(source string) error {
				if kind == "post" {
					p, err := store.Save(t.Context(), Post{ID: id, Version: version, Slug: "post", Title: "Post", Markdown: source})
					if err == nil {
						id, version = p.ID, p.Version
					}
					return err
				}
				p, err := store.SavePage(t.Context(), Page{ID: id, Version: version, Slug: "page", Title: "Page", Published: true, Markdown: source})
				if err == nil {
					id, version = p.ID, p.Version
				}
				return err
			}
			if err := save("![Original](/images/original.png)"); err != nil {
				t.Fatal(err)
			}
			table := "blog_" + kind + "_images"
			if _, err := store.db.Exec("CREATE TRIGGER fail_image_reference BEFORE INSERT ON " + table + " BEGIN SELECT RAISE(ABORT, 'test reference failure'); END"); err != nil {
				t.Fatal(err)
			}
			if err := save("![Replacement](/images/replacement.png)"); err == nil {
				t.Fatal("reference failure reported success")
			}
			if _, err := store.db.Exec("DROP TRIGGER fail_image_reference"); err != nil {
				t.Fatal(err)
			}
			var source string
			if err := store.db.QueryRow("SELECT markdown FROM blog_"+kind+"s WHERE id=?", id).Scan(&source); err != nil || source != "![Original](/images/original.png)" {
				t.Fatal("failed index update changed content", source, err)
			}
			for _, ref := range []string{"original", "replacement"} {
				public, err := store.ImageIsPublic(t.Context(), "/images/"+ref+".png")
				if err != nil || public != (ref == "original") {
					t.Fatal("failed update changed access", ref, public, err)
				}
			}
			var err error
			if kind == "post" {
				err = store.Delete(t.Context(), id)
			} else {
				err = store.DeletePage(t.Context(), id)
			}
			if err != nil {
				t.Fatal(err)
			}
			var count int
			if err := store.db.QueryRow("SELECT count(*) FROM "+table+" WHERE owner_id=?", id).Scan(&count); err != nil || count != 0 {
				t.Fatal("delete left reference rows", count, err)
			}
		})
	}
}

// TestImageReferenceBackfill covers existing content, draft filtering, and reopen.
func TestImageReferenceBackfill(t *testing.T) {
	directory := t.TempDir()
	settings := Settings{}
	settings.Storage.Provider, settings.Storage.Connection = "sqlite", "blog.db"
	store, closeStore, err := openRepository(t.Context(), settings, directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeStore() })
	for _, draft := range []bool{false, true} {
		slug := "live"
		if draft {
			slug = "draft"
		}
		if _, err := store.Save(t.Context(), Post{Slug: slug, Title: slug, Draft: draft, PreviewImage: slug + ".png"}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.SavePage(t.Context(), Page{Slug: slug, Title: slug, Published: !draft, Markdown: "![Image](/images/page-" + slug + ".png)"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.db.Exec("DROP TABLE blog_post_images; DROP TABLE blog_page_images"); err != nil {
		t.Fatal(err)
	}
	closeStore()
	for range 2 {
		store, closeStore, err = openRepository(t.Context(), settings, directory)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{"live", "draft", "page-live", "page-draft"} {
			public, err := store.ImageIsPublic(t.Context(), "/images/"+id+".png")
			if err != nil || public != strings.HasSuffix(id, "live") {
				t.Fatal("incorrect backfilled visibility", id, public, err)
			}
		}
		if err := closeStore(); err != nil {
			t.Fatal(err)
		}
	}
}
