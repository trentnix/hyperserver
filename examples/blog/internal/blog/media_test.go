package blog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// mediaUpload creates a distinct upload and returns its deletion version.
func mediaUpload(t *testing.T, m *module, label string) mediaFile {
	t.Helper()
	pixels := image.NewRGBA(image.Rect(0, 0, 2, 2))
	digest := sha256.Sum256([]byte(label))
	copy(pixels.Pix, digest[:])
	var data bytes.Buffer
	if err := png.Encode(&data, pixels); err != nil {
		t.Fatal(err)
	}
	id, err := m.images.Put(t.Context(), "image/png", data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	image, err := m.images.Open(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	defer image.Content.Close()
	return mediaFile{ID: id, Modified: image.Modified}
}

// TestMediaReferences protects every saved use, including hidden descendants and history.
func TestMediaReferences(t *testing.T) {
	h := newHarness(t)
	m := h.blog
	want := make(map[string]bool)
	add := func(label string) string {
		id := mediaUpload(t, m, label).ID
		want[id] = true
		return id
	}
	for _, state := range []string{"published", "draft", "scheduled"} {
		id := add(state)
		p := Post{Slug: state, Title: state, Markdown: "![Image](/images/" + id + "?size=1#image)", Draft: state == "draft"}
		if state == "scheduled" {
			p.Published = time.Now().Add(time.Hour)
		}
		if _, err := m.posts.Save(t.Context(), p); err != nil {
			t.Fatal(err)
		}
	}
	parent, err := m.pageStore.SavePage(t.Context(), Page{Slug: "hidden", Title: "Hidden"})
	if err != nil {
		t.Fatal(err)
	}
	for _, published := range []bool{false, true} {
		id := add(fmt.Sprint("page", published))
		if _, err := m.pageStore.SavePage(t.Context(), Page{Slug: fmt.Sprint("child-", published), Title: "Child", Published: published, ParentID: parent.ID, PreviewImage: id}); err != nil {
			t.Fatal(err)
		}
	}
	profile := Profile{Name: "Profile", ImageURL: "/images/" + add("portrait"), FaviconURL: "https://blog.example/images/" + add("favicon"), BackgroundURL: "/images/" + add("background")}
	if err := m.writeProfile(t.Context(), profile); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"post", "page"} {
		body, preview := add(kind+" history body"), add(kind+" history preview")
		if kind == "post" {
			p, err := m.posts.Save(t.Context(), Post{Slug: "history", Title: "Old", Markdown: "![Old](../../images/" + body + ")", PreviewImage: preview, Draft: true})
			if err != nil {
				t.Fatal(err)
			}
			p.Markdown, p.PreviewImage = "No images", ""
			if _, err := m.posts.Save(t.Context(), p); err != nil {
				t.Fatal(err)
			}
		} else {
			p, err := m.pageStore.SavePage(t.Context(), Page{Slug: "history", Title: "Old", Markdown: "![Old](../images/" + body + ")", PreviewImage: preview})
			if err != nil {
				t.Fatal(err)
			}
			p.Markdown, p.PreviewImage = "No images", ""
			if _, err := m.pageStore.SavePage(t.Context(), p); err != nil {
				t.Fatal(err)
			}
		}
	}
	unreferenced := mediaUpload(t, m, "unused")
	used, err := m.mediaReferences.ReferencedImages(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, found := used[unreferenced.ID]; len(used) != len(want) || found {
		t.Fatalf("references=%v want=%v", used, want)
	}
	for id := range want {
		if _, found := used[id]; !found {
			t.Errorf("lost reference: %s", id)
		}
		if err := m.removeMedia(t.Context(), id, 0, false); !errors.Is(err, errMediaUsed) && !errors.Is(err, errMediaHistory) {
			t.Errorf("removed used image: %v", err)
		}
		if used[id].Current {
			if err := m.removeMedia(t.Context(), id, 0, true); !errors.Is(err, errMediaUsed) {
				t.Errorf("history acknowledgment bypassed current use: %v", err)
			}
		}
	}
	if err := m.removeMedia(t.Context(), unreferenced.ID, unreferenced.Version(), false); err != nil {
		t.Fatal(err)
	}
}

// TestMediaHistoryRelease retains an image until its last saved version is removed.
func TestMediaHistoryRelease(t *testing.T) {
	for _, kind := range []string{"post", "page"} {
		t.Run(kind, func(t *testing.T) {
			h := newHarness(t)
			f := mediaUpload(t, h.blog, kind)
			var id string
			if kind == "post" {
				p, err := h.blog.posts.Save(t.Context(), Post{Slug: "history", Title: "Old", PreviewImage: f.ID, Draft: true})
				if err != nil {
					t.Fatal(err)
				}
				id, p.PreviewImage = p.ID, ""
				if _, err := h.blog.posts.Save(t.Context(), p); err != nil {
					t.Fatal(err)
				}
			} else {
				p, err := h.blog.pageStore.SavePage(t.Context(), Page{Slug: "history", Title: "Old", PreviewImage: f.ID})
				if err != nil {
					t.Fatal(err)
				}
				id, p.PreviewImage = p.ID, ""
				if _, err := h.blog.pageStore.SavePage(t.Context(), p); err != nil {
					t.Fatal(err)
				}
			}
			if err := h.blog.removeMedia(t.Context(), f.ID, f.Version(), false); !errors.Is(err, errMediaHistory) {
				t.Fatal(err)
			}
			versions, err := h.blog.history.ListRevisions(t.Context(), kind, id)
			if err != nil {
				t.Fatal(err)
			}
			if err := h.blog.history.DeleteRevision(t.Context(), kind, id, versions[1].ID); err != nil {
				t.Fatal(err)
			}
			if err := h.blog.removeMedia(t.Context(), f.ID, f.Version(), false); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestMediaCleanupHTTP covers authorization, confirmation, stale requests, and ordinary/HTMX responses.
func TestMediaCleanupHTTP(t *testing.T) {
	h := newHarness(t)
	f := mediaUpload(t, h.blog, "unused")
	base := "/media/delete/" + f.ID
	values := url.Values{"version": {strconv.FormatInt(f.Version(), 10)}}
	for _, state := range []string{"anonymous", "unverified"} {
		if state == "unverified" {
			h.register(t)
			h.login(t)
		}
		for _, method := range []string{"GET", "POST"} {
			w := h.request(method, base, values, false)
			requireStatus(t, w, http.StatusForbidden)
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("cacheable denial")
			}
		}
		requireStatus(t, h.request("GET", "/media/unused", nil, false), http.StatusForbidden)
	}
	h.verify(t)
	request := httptest.NewRequest("POST", "https://blog.example"+base, strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", "https://attacker.example")
	for _, cookie := range h.cookies {
		request.AddCookie(cookie)
	}
	denied := httptest.NewRecorder()
	h.app.Handler.ServeHTTP(denied, request)
	requireStatus(t, denied, http.StatusForbidden)
	w := h.request("GET", "/media/unused", nil, false)
	requireStatus(t, w, http.StatusOK)
	if !strings.Contains(w.Body.String(), f.ID) || !strings.Contains(w.Body.String(), "browser recovery copies") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing cleanup UI or privacy")
	}
	w = h.request("GET", base, nil, false)
	requireStatus(t, w, http.StatusOK)
	if !strings.Contains(w.Body.String(), `name="version" value="`+values.Get("version")+`"`) {
		t.Fatal("missing confirmation version")
	}
	requireStatus(t, h.request("POST", base, nil, false), http.StatusBadRequest)

	// A reference added after confirmation must win over the old deletion form.
	p, err := h.blog.posts.Save(t.Context(), Post{Slug: "retained", Title: "Retained", Draft: true, PreviewImage: f.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, hx := range []bool{false, true} {
		requireStatus(t, h.request("POST", base, values, hx), http.StatusConflict)
	}
	requireStatus(t, h.request("GET", base, nil, false), http.StatusConflict)
	if err := h.blog.posts.Delete(t.Context(), p.ID); err != nil {
		t.Fatal(err)
	}
	for _, hx := range []bool{false, true} {
		file := mediaUpload(t, h.blog, fmt.Sprint("delete", hx))
		w := h.request("POST", "/media/delete/"+file.ID, url.Values{"version": {strconv.FormatInt(file.Version(), 10)}}, hx)
		if hx {
			if w.Header().Get("HX-Redirect") != "/media/unused" {
				t.Fatal(w.Header())
			}
		} else {
			requireStatus(t, w, http.StatusSeeOther)
		}
		if _, err := h.blog.images.Open(t.Context(), file.ID); !errors.Is(err, errNotFound) {
			t.Fatal("file not removed", err)
		}
		requireStatus(t, h.request("POST", "/media/delete/"+file.ID, url.Values{"version": {strconv.FormatInt(file.Version(), 10)}}, hx), http.StatusNotFound)
	}
}

// TestMediaFileSafety checks generated names, replacement detection, cancellation, and filesystem failures.
func TestMediaFileSafety(t *testing.T) {
	s := newTestImageStore(t)
	id, err := s.Put(t.Context(), "image/png", []byte("image"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := s.List(t.Context())
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
	for _, name := range []string{".upload-pending", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(s.root.Name(), name), []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	directory, symlink := strings.Repeat("a", 64)+".png", strings.Repeat("b", 64)+".png"
	if err := s.root.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(id, filepath.Join(s.root.Name(), symlink)); err != nil {
		t.Fatal(err)
	}
	listed, err := s.List(t.Context())
	if err != nil || len(listed) != 1 || listed[0].ID != id {
		t.Fatal(listed, err)
	}
	for _, name := range []string{"../" + id, "/" + id, ".upload-pending", "notes.txt", directory, symlink} {
		if err := s.Remove(t.Context(), name, files[0].Version()); !errors.Is(err, errNotFound) {
			t.Errorf("unsafe removal %s: %v", name, err)
		}
	}
	if err := s.Remove(t.Context(), id, files[0].Version()+1); !errors.Is(err, errMediaChanged) {
		t.Fatal(err)
	}
	changed := files[0].Modified.Add(time.Second)
	if err := os.Chtimes(filepath.Join(s.root.Name(), id), changed, changed); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(t.Context(), id, files[0].Version()); !errors.Is(err, errMediaChanged) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.List(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := s.Remove(ctx, id, changed.UnixNano()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := s.Remove(t.Context(), id, changed.UnixNano()); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.List(t.Context()); err == nil {
		t.Fatal("closed store listed")
	}
	if err := s.Remove(t.Context(), id, 0); err == nil {
		t.Fatal("closed store removed")
	}
}

type failedMediaReferences struct{}

func (failedMediaReferences) ReferencedImages(context.Context) (map[string]mediaUsage, error) {
	return nil, errors.New("private database failure")
}

type failedMediaStore struct{ mediaStore }

func (failedMediaStore) Remove(context.Context, string, int64) error {
	return errors.New("private file failure")
}

type failedMediaList struct{ mediaStore }

func (failedMediaList) List(context.Context) ([]mediaFile, error) {
	return nil, errors.New("private list failure")
}

// TestMediaCleanupFailures never reports success or removes files after storage failures.
func TestMediaCleanupFailures(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	f := mediaUpload(t, h.blog, "keep")
	refs, files := h.blog.mediaReferences, h.blog.media
	for _, failure := range []string{"references", "files"} {
		h.blog.mediaReferences, h.blog.media = refs, files
		if failure == "references" {
			h.blog.mediaReferences = failedMediaReferences{}
		} else {
			h.blog.media = failedMediaStore{files}
		}
		w := h.request("POST", "/media/delete/"+f.ID, url.Values{"version": {strconv.FormatInt(f.Version(), 10)}}, false)
		requireStatus(t, w, http.StatusInternalServerError)
		if strings.Contains(w.Body.String(), "private ") || w.Header().Get("Location") != "" {
			t.Fatal("leaked failure or redirected")
		}
		image, err := h.blog.images.Open(t.Context(), f.ID)
		if err != nil {
			t.Fatal("failed operation removed image", err)
		}
		image.Content.Close()
	}
	h.blog.mediaReferences, h.blog.media = refs, failedMediaList{files}
	requireStatus(t, h.request("GET", "/media/unused", nil, false), http.StatusInternalServerError)
	h.blog.mediaReferences, h.blog.media = failedMediaReferences{}, files
	requireStatus(t, h.request("GET", "/media/unused", nil, false), http.StatusInternalServerError)
	requireStatus(t, h.request("GET", "/media/delete/"+f.ID, nil, false), http.StatusInternalServerError)
}

// TestMediaReferenceScanFailure rejects incomplete scans rather than deleting based on partial results.
func TestMediaReferenceScanFailure(t *testing.T) {
	h := newHarness(t)
	f := mediaUpload(t, h.blog, "keep after scan failure")
	store := h.blog.posts.(*sqliteRepository)
	if _, err := store.db.ExecContext(t.Context(), "DROP TABLE blog_page_revisions"); err != nil {
		t.Fatal(err)
	}
	if err := h.blog.removeMedia(t.Context(), f.ID, f.Version(), false); err == nil {
		t.Fatal("incomplete reference check succeeded")
	}
	image, err := h.blog.images.Open(t.Context(), f.ID)
	if err != nil {
		t.Fatal(err)
	}
	image.Content.Close()
}

type blockedPostSave struct {
	repository
	entered, release chan struct{}
}

func (s blockedPostSave) Save(ctx context.Context, p Post) (Post, error) {
	close(s.entered)
	select {
	case <-s.release:
	case <-ctx.Done():
		return Post{}, ctx.Err()
	}
	return s.repository.Save(ctx, p)
}

type blockedPageSave struct {
	pageRepository
	entered, release chan struct{}
}

func (s blockedPageSave) SavePage(ctx context.Context, p Page) (Page, error) {
	close(s.entered)
	select {
	case <-s.release:
	case <-ctx.Done():
		return Page{}, ctx.Err()
	}
	return s.pageRepository.SavePage(ctx, p)
}

// TestMediaCleanupDuringContentSave keeps a validated image until its post or page commits.
func TestMediaCleanupDuringContentSave(t *testing.T) {
	for _, kind := range []string{"post", "pages"} {
		t.Run(kind, func(t *testing.T) {
			h := newHarness(t)
			h.register(t)
			h.verify(t)
			h.login(t)
			f := mediaUpload(t, h.blog, kind)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			entered, release := make(chan struct{}), make(chan struct{})
			if kind == "post" {
				h.blog.posts = blockedPostSave{h.blog.posts, entered, release}
			} else {
				h.blog.pageStore = blockedPageSave{h.blog.pageStore, entered, release}
			}
			values := url.Values{"title": {"Saved"}, "slug": {"saved"}, "content": {"![Saved](/images/" + f.ID + ")"}}
			request := httptest.NewRequest("POST", "https://blog.example/"+kind, strings.NewReader(values.Encode())).WithContext(ctx)
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			for _, cookie := range h.cookies {
				request.AddCookie(cookie)
			}
			w := httptest.NewRecorder()
			saved, removed := make(chan struct{}), make(chan error, 1)
			go func() { h.app.Handler.ServeHTTP(w, request); close(saved) }()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			go func() { removed <- h.blog.removeMedia(ctx, f.ID, f.Version(), false) }()
			close(release)
			<-saved
			requireStatus(t, w, http.StatusSeeOther)
			if err := <-removed; !errors.Is(err, errMediaUsed) {
				t.Fatal("save lost its image", err)
			}
		})
	}
}

type blockedProfileSave struct {
	profileRepository
	entered, release chan struct{}
}

func (s blockedProfileSave) SaveProfile(ctx context.Context, p Profile) error {
	close(s.entered)
	select {
	case <-s.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return s.profileRepository.SaveProfile(ctx, p)
}

type blockedMediaRemove struct {
	mediaStore
	entered, release chan struct{}
}

func (s blockedMediaRemove) Remove(ctx context.Context, id string, version int64) error {
	close(s.entered)
	select {
	case <-s.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return s.mediaStore.Remove(ctx, id, version)
}

// TestMediaCleanupConcurrentSave tests both orderings of a save and cleanup.
func TestMediaCleanupConcurrentSave(t *testing.T) {
	for _, saveFirst := range []bool{true, false} {
		t.Run(fmt.Sprint(saveFirst), func(t *testing.T) {
			h := newHarness(t)
			f := mediaUpload(t, h.blog, "concurrent")
			p := Profile{Name: "Concurrent", ImageURL: "/images/" + f.ID}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			entered, release := make(chan struct{}), make(chan struct{})
			if saveFirst {
				h.blog.profileStore = blockedProfileSave{h.blog.profileStore, entered, release}
			} else {
				h.blog.media = blockedMediaRemove{h.blog.media, entered, release}
			}
			saved, removed := make(chan error, 1), make(chan error, 1)
			save := func() { saved <- h.blog.writeProfile(ctx, p) }
			remove := func() { removed <- h.blog.removeMedia(ctx, f.ID, f.Version(), true) }
			if saveFirst {
				go save()
			} else {
				go remove()
			}
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if saveFirst {
				go remove()
			} else {
				go save()
			}
			close(release)
			saveErr, removeErr := <-saved, <-removed
			if saveFirst {
				if saveErr != nil || !errors.Is(removeErr, errMediaUsed) {
					t.Fatalf("save=%v remove=%v", saveErr, removeErr)
				}
			} else {
				if !errors.Is(saveErr, errMissingMedia) || removeErr != nil {
					t.Fatalf("save=%v remove=%v", saveErr, removeErr)
				}
				if h.blog.profile.Current().ImageURL == p.ImageURL {
					t.Fatal("saved deleted image")
				}
			}
		})
	}
}

// TestMediaStaleEditors prevents saves from recreating broken local references after deletion.
func TestMediaStaleEditors(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	f := mediaUpload(t, h.blog, "deleted")
	if err := h.blog.removeMedia(t.Context(), f.ID, f.Version(), false); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"/images/" + f.ID, "https://blog.example/images/" + f.ID + "?size=1", "//blog.example:443/images/" + f.ID} {
		for _, path := range []string{"/post", "/pages"} {
			w := h.request("POST", path, url.Values{"title": {"Keep my writing"}, "slug": {"unsaved"}, "content": {"![Missing](" + ref + ")"}}, true)
			requireStatus(t, w, http.StatusUnprocessableEntity)
			if !strings.Contains(w.Body.String(), "Upload it again") {
				t.Fatal(w.Body.String())
			}
		}
		w := h.request("POST", "/profile", url.Values{"name": {"Keep my name"}, "imageURL": {ref}}, false)
		// Profile validation rejects protocol-relative URLs before the media check.
		requireStatus(t, w, http.StatusUnprocessableEntity)
	}
	if err := h.blog.checkMediaReferences(t.Context(), []string{"https://remote.example/images/" + f.ID}); err != nil {
		t.Fatal("external image fetched", err)
	}
	if _, err := h.blog.posts.GetBySlug(t.Context(), "unsaved", true); !errors.Is(err, errNotFound) {
		t.Fatal("invalid post persisted", err)
	}
	if _, err := h.blog.pageStore.GetPageBySlug(t.Context(), "unsaved", true); !errors.Is(err, errNotFound) {
		t.Fatal("invalid page persisted", err)
	}
}

// TestMediaReferenceURLs preserves equivalent local references and old-domain links.
func TestMediaReferenceURLs(t *testing.T) {
	id := strings.Repeat("a", 64) + ".png"
	for _, reference := range []string{"/images/" + id, "https://old.example/images/" + id + "?x=1#fragment", "//blog.example/images/" + id, "/images/%61" + id[1:]} {
		if got := referencedImageID(reference); got != id {
			t.Errorf("%q: %q", reference, got)
		}
	}
	for _, reference := range []string{"", "/assets/" + id, "/images/../../" + id, "/images/bad.png", "data:image/png,content"} {
		if got := referencedImageID(reference); got != "" {
			t.Errorf("%q: %q", reference, got)
		}
	}
}

// TestMediaValidationListener checks absolute references when publicOrigin is omitted.
func TestMediaValidationListener(t *testing.T) {
	h := newHarness(t)
	f := mediaUpload(t, h.blog, "missing listener image")
	if err := h.blog.removeMedia(t.Context(), f.ID, f.Version(), false); err != nil {
		t.Fatal(err)
	}
	h.app.Config.HTTP.PublicOrigin = ""
	for _, secure := range []bool{false, true} {
		h.app.Config.HTTP.TLS.Enabled = secure
		scheme := "http"
		h.app.Config.HTTP.Port = 80
		if secure {
			scheme = "https"
			h.app.Config.HTTP.Port = 443
		}
		for _, ref := range []string{scheme + "://127.0.0.1/images/" + f.ID, fmt.Sprintf("//127.0.0.1:%d/images/%s", h.app.Config.HTTP.Port, f.ID)} {
			if err := h.blog.checkMediaReferences(t.Context(), []string{ref}); !errors.Is(err, errMissingMedia) {
				t.Fatalf("%s: %v", ref, err)
			}
		}
	}
}
