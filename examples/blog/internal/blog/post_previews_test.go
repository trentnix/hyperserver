package blog

import (
	"bytes"
	"database/sql"
	"image"
	"image/png"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

// TestPostPreviewsHTTP covers selection, replacement, removal, draft privacy,
// and validation without changing the visible article or publication date.
func TestPostPreviewsHTTP(t *testing.T) {
	for _, hx := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "HTMX"}[hx], func(t *testing.T) {
			h := newHarness(t)
			imageID, err := h.blog.images.Put(t.Context(), testImageUpload("image/png", testPNG(t)))
			if err != nil {
				t.Fatal(err)
			}
			fields := url.Values{"slug": {"preview"}, "title": {"Preview"}, "content": {"![Content](/content.jpg)"}, "previewImage": {imageID}, "published": {"on"}}
			requireStatus(t, h.request("POST", "/post", fields, hx), http.StatusForbidden)
			h.register(t)
			h.verify(t)
			h.login(t)
			status := http.StatusSeeOther
			if hx {
				status = http.StatusOK
			}
			requireStatus(t, h.request("POST", "/post", fields, hx), status)
			p, err := h.blog.posts.GetBySlug(t.Context(), "preview", true)
			if err != nil || p.PreviewImage != imageID {
				t.Fatal("preview not saved", p, err)
			}
			response := h.request("GET", "/post/s/preview", nil, false)
			if sharingTags(t, response.Body.String())["og:image"] != "https://blog.example/images/"+imageID {
				t.Fatal("selected preview did not take priority")
			}
			_, body, _ := strings.Cut(response.Body.String(), "</head>")
			if strings.Contains(body, imageID) || !strings.Contains(body, "/content.jpg") {
				t.Fatal("preview changed the visible article")
			}
			response = h.request("GET", "/post/update/"+p.ID, nil, hx)
			if !strings.Contains(response.Body.String(), `name="previewImage" value="`+imageID+`"`) {
				t.Fatal("editor lost selection")
			}

			// Invalid selections and storage failures must leave the saved post intact.
			for _, invalid := range []string{"missing.png", "../../private", "https://other.example/image.png"} {
				fields.Set("previewImage", invalid)
				requireStatus(t, h.submitEditor(t, "/post/"+p.ID, fields, hx), http.StatusUnprocessableEntity)
			}
			fields.Set("previewImage", imageID)
			fields.Set("title", "")
			response = h.submitEditor(t, "/post/"+p.ID, fields, hx)
			requireStatus(t, response, http.StatusUnprocessableEntity)
			if !hx && !strings.Contains(response.Body.String(), `name="previewImage" value="`+imageID+`"`) {
				t.Fatal("validation failure lost selection")
			}
			fields.Set("title", "Preview")
			images := h.blog.images
			h.blog.images = failingImageStore{}
			requireStatus(t, h.submitEditor(t, "/post/"+p.ID, fields, hx), http.StatusInternalServerError)
			h.blog.images = images
			unchanged, err := h.blog.posts.GetByID(t.Context(), p.ID, true)
			if err != nil || unchanged != p {
				t.Fatal("failed save changed stored content", unchanged, err)
			}

			var data bytes.Buffer
			if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 3, 3))); err != nil {
				t.Fatal(err)
			}
			replacement, err := images.Put(t.Context(), testImageUpload("image/png", data.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			fields.Set("previewImage", replacement)
			requireStatus(t, h.submitEditor(t, "/post/"+p.ID, fields, hx), status)
			saved, err := h.blog.posts.GetByID(t.Context(), p.ID, true)
			if err != nil || saved.PreviewImage != replacement || !saved.Published.Equal(p.Published) {
				t.Fatal("replacement changed publication date or failed", saved, err)
			}
			fields.Del("published")
			requireStatus(t, h.submitEditor(t, "/post/"+p.ID, fields, hx), status)
			if tags := sharingTags(t, h.request("GET", "/post/s/preview", nil, false).Body.String()); len(tags) != 0 {
				t.Fatal("draft advertised a preview", tags)
			}
			fields.Set("published", "on")
			fields.Set("previewImage", "")
			requireStatus(t, h.submitEditor(t, "/post/"+p.ID, fields, hx), status)
			if tags := sharingTags(t, h.request("GET", "/post/s/preview", nil, false).Body.String()); tags["og:image"] != "https://blog.example/content.jpg" {
				t.Fatal("removing selection did not restore automatic image", tags)
			}
		})
	}
}

// TestPostPreviewUpgrade preserves existing posts and saves selections across restarts.
func TestPostPreviewUpgrade(t *testing.T) {
	directory := t.TempDir()
	db, err := sql.Open("sqlite3", filepath.Join(directory, "blog.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE blog_posts (id TEXT PRIMARY KEY, slug TEXT NOT NULL UNIQUE, title TEXT NOT NULL, summary TEXT NOT NULL, markdown TEXT NOT NULL, published INTEGER NOT NULL, draft INTEGER NOT NULL);
INSERT INTO blog_posts VALUES ('old', 'old', 'Title', 'Summary', 'Body', 1700000000, 0)`)
	closeErr := db.Close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	settings := Settings{}
	settings.Storage.Provider, settings.Storage.Connection = "sqlite", "blog.db"
	for iteration := range 2 {
		store, closeStore, err := openRepository(t.Context(), settings, directory)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { closeStore() })
		p, err := store.GetByID(t.Context(), "old", true)
		wantImage := ""
		if iteration == 1 {
			wantImage = "selected.png"
		}
		if err != nil || p.PreviewImage != wantImage || p.Published.Unix() != 1700000000 || p.Markdown != "Body" || p.Draft {
			t.Fatal("upgrade or reopen changed post", p, err)
		}
		p.PreviewImage = "selected.png"
		if _, err := store.Save(t.Context(), p); err != nil {
			t.Fatal(err)
		}
		if err := closeStore(); err != nil {
			t.Fatal(err)
		}
	}
}
