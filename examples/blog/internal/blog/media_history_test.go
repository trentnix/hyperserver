package blog

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestMediaHistoryDeletion exercises explicit deletion and restoration through both editors.
func TestMediaHistoryDeletion(t *testing.T) {
	for _, kind := range []string{"post", "page"} {
		t.Run(kind, func(t *testing.T) {
			h := newHarness(t)
			m := h.blog
			store := m.posts.(*sqliteRepository)
			file := mediaUpload(t, m, kind+" historical")
			body := "![Old](/images/" + file.ID + ")\n\n[Download](https://blog.example/images/" + file.ID + ")"
			original := Revision{Title: "Earlier <title>", Markdown: body, PreviewImage: file.ID}
			id, version, err := saveRevisionContent(t.Context(), store, kind, "", 0, original)
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = saveRevisionContent(t.Context(), store, kind, id, version, Revision{Title: "Current", Markdown: "No images"})
			if err != nil {
				t.Fatal(err)
			}
			versions, err := store.ListRevisions(t.Context(), kind, id)
			if err != nil || len(versions) != 2 {
				t.Fatal(versions, err)
			}
			selected := strconv.FormatInt(versions[1].ID, 10)
			historyURL := "/history/" + kind + "/" + id + "?revision=" + selected
			h.register(t)
			h.verify(t)
			h.login(t)

			candidates, err := m.mediaCandidates(t.Context())
			if err != nil || len(candidates.Files) != 0 || len(candidates.History) != 1 || len(candidates.History[0].History) != 1 {
				t.Fatalf("incorrect grouping or duplicate revision: %+v, %v", candidates, err)
			}
			usage := candidates.History[0].History[0]
			if usage.URL != historyURL || !usage.RecordedAt.Equal(versions[1].RecordedAt) {
				t.Fatal("incorrect history link or date", usage)
			}
			listing := h.request("GET", "/media/unused", nil, false)
			requireStatus(t, listing, http.StatusOK)
			if !strings.Contains(listing.Body.String(), "Used only by history") || strings.Contains(listing.Body.String(), `href="/media/delete-unused"`) {
				t.Fatal("history-only file exposed as unused")
			}
			bulk := h.request("POST", "/media/delete-unused", mediaBatchForm(file), false)
			requireStatus(t, bulk, http.StatusOK)
			if !strings.Contains(bulk.Body.String(), "Deleted: 0. Skipped: 1.") {
				t.Fatal("bulk cleanup removed historical media")
			}
			path := "/media/delete/" + file.ID
			confirmation := h.request("GET", path, nil, false)
			requireStatus(t, confirmation, http.StatusOK)
			for _, want := range []string{historyURL, "Earlier &lt;title&gt;", `name="deleteHistory"`, "writing will remain"} {
				if !strings.Contains(confirmation.Body.String(), want) {
					t.Fatal("missing confirmation detail", want)
				}
			}
			values := url.Values{"version": {strconv.FormatInt(file.Version(), 10)}}
			requireStatus(t, h.request("POST", path, values, false), http.StatusConflict)
			values.Set("deleteHistory", "true")
			values.Set("version", strconv.FormatInt(file.Version()-int64(time.Second), 10))
			requireStatus(t, h.request("POST", path, values, false), http.StatusConflict)
			values.Set("version", strconv.FormatInt(file.Version(), 10))
			requireStatus(t, h.request("POST", path, values, false), http.StatusSeeOther)
			if _, err := m.images.Open(t.Context(), file.ID); !errors.Is(err, errNotFound) {
				t.Fatal("historical image not deleted", err)
			}
			after, err := store.ListRevisions(t.Context(), kind, id)
			if err != nil || !reflect.DeepEqual(versions, after) {
				t.Fatal("media deletion changed history", err)
			}

			for _, hx := range []bool{false, true} {
				preview := h.request("GET", historyURL, nil, hx)
				requireStatus(t, preview, http.StatusOK)
				text := preview.Body.String()
				if strings.Count(text, `class="missing-image"`) != 2 || !strings.Contains(text, "saved writing has not changed") || strings.Contains(text, `src="/images/`+file.ID+`"`) {
					t.Fatal("missing preview placeholders", text)
				}
			}
			prefix := "/post"
			if kind == "page" {
				prefix = "/pages"
			}
			editor := h.request("GET", prefix+"/update/"+id+"?revision="+selected, nil, false)
			requireStatus(t, editor, http.StatusOK)
			if !strings.Contains(editor.Body.String(), "Replace or remove their references before saving.") {
				t.Fatal("editor lacks missing-image warning")
			}
			form := revisionForm(t, editor.Body.String())
			form.Set("publication", "draft")
			form.Set("childrenDisplay", "hidden")
			form.Set("parentID", "")
			endpoint := prefix + "/" + id + "?revision=" + selected
			for _, hx := range []bool{false, true} {
				requireStatus(t, h.request("POST", endpoint, form, hx), http.StatusUnprocessableEntity)
			}
			form.Set("previewImage", "")
			response := h.request("POST", endpoint, form, false)
			requireStatus(t, response, http.StatusUnprocessableEntity)
			if !strings.Contains(response.Body.String(), "An uploaded image is no longer available.") {
				t.Fatal("missing body image was not explained")
			}
			form.Set("content", "Recovered writing without the deleted image")
			requireStatus(t, h.request("POST", endpoint, form, false), http.StatusSeeOther)
		})
	}
}

// TestMediaHistoryConfirmationCannotBypassCurrentUse checks stale or tampered confirmations.
func TestMediaHistoryConfirmationCannotBypassCurrentUse(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	f := mediaUpload(t, h.blog, "now used")
	values := url.Values{"version": {strconv.FormatInt(f.Version(), 10)}, "deleteHistory": {"true"}}
	for _, state := range []string{"profile", "draft"} {
		if state == "draft" {
			if err := h.blog.writeProfile(t.Context(), Profile{Name: "Profile"}); err != nil {
				t.Fatal(err)
			}
			if _, err := h.blog.posts.Save(t.Context(), Post{Title: "Current", Slug: "current", Draft: true, PreviewImage: f.ID}); err != nil {
				t.Fatal(err)
			}
		} else if err := h.blog.writeProfile(t.Context(), Profile{Name: "Profile", ImageURL: "/images/" + f.ID}); err != nil {
			t.Fatal(err)
		}
		requireStatus(t, h.request("POST", "/media/delete/"+f.ID, values, false), http.StatusConflict)
	}
}

// failedMediaOpen verifies that unavailable storage is not presented as deleted files.
type failedMediaOpen struct{ imageStore }

func (failedMediaOpen) Open(context.Context, string) (storedImage, error) {
	return storedImage{}, errors.New("private storage failure")
}

// TestRevisionMissingMedia handles relative URLs and preserves external images and safe HTML.
func TestRevisionMissingMedia(t *testing.T) {
	h := newHarness(t)
	f := mediaUpload(t, h.blog, "missing history image")
	if err := h.blog.removeMedia(t.Context(), f.ID, f.Version(), false); err != nil {
		t.Fatal(err)
	}
	revision := Revision{Markdown: "![Deleted](../../images/" + f.ID + "?x=1#here)\n\n![External](https://remote.example/images/" + f.ID + ")\n\n<script>alert(1)</script>"}
	path := "/post/s/example"
	missing, err := h.blog.revisionMissingImages(t.Context(), revision, path)
	if err != nil || len(missing) != 1 {
		t.Fatal(missing, err)
	}
	rendered, err := markdownAt(revision.Markdown, &url.URL{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	marked, err := markMissingImages(rendered, missing)
	if err != nil || strings.Count(string(marked), `class="missing-image"`) != 1 || !strings.Contains(string(marked), `src="https://remote.example/images/`) || strings.Contains(string(marked), "<script>") {
		t.Fatal("incorrect or unsafe placeholders", marked, err)
	}
	h.blog.images = failedMediaOpen{h.blog.images}
	if _, err := h.blog.revisionMissingImages(t.Context(), revision, path); err == nil {
		t.Fatal("storage failure mistaken for missing media")
	}
}
