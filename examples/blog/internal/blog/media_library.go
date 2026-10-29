package blog

import (
	"cmp"
	"net/http"
	"slices"
	"strconv"
)

const mediaPageSize = 24

// mediaDetailPage pairs an upload with its current and historical references.
type mediaDetailPage struct {
	ID    string
	Usage mediaUsage
}

// mediaDetail shows why an image is retained without changing its visibility.
func (m *module) mediaDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	image, err := m.images.Open(r.Context(), id)
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	image.Content.Close()
	used, err := m.mediaReferences.ReferencedImages(r.Context())
	if err != nil {
		m.storageError(w, r, err)
		return
	}

	page := m.views["mediaDetail"].Content(r, mediaDetailPage{ID: id, Usage: used[id]})
	page.Title = "Image details"
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display this image.", err)
	}
}

// mediaLibraryPage describes a bounded page of existing uploads, not their public visibility.
type mediaLibraryPage struct {
	Files          []mediaFile
	Page, Pages    int
	Previous, Next string
	Picking        bool
}

// mediaLibrary lets only the administrator browse uploads, including private images.
// Selecting a file does not reserve it. Normal save validation rechecks its existence.
func (m *module) mediaLibrary(w http.ResponseWriter, r *http.Request) {
	number := 1
	var err error
	if raw := r.URL.Query().Get("page"); raw != "" {
		number, err = strconv.Atoi(raw)
	}
	if err != nil || number < 1 {
		http.Error(w, "Invalid media page.", http.StatusBadRequest)
		return
	}

	files, err := m.media.List(r.Context())
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	slices.SortFunc(files, func(a, b mediaFile) int {
		if order := b.Modified.Compare(a.Modified); order != 0 {
			return order
		}
		return cmp.Compare(a.ID, b.ID)
	})
	data := mediaLibraryPage{Page: number, Pages: max(1, (len(files)+mediaPageSize-1)/mediaPageSize), Picking: r.URL.Query().Get("pick") == "1"}
	if number > data.Pages {
		m.storageError(w, r, errNotFound)
		return
	}
	start := (number - 1) * mediaPageSize
	data.Files = files[start:min(start+mediaPageSize, len(files))]
	pageURL := func(number int) string {
		path := "/media?page=" + strconv.Itoa(number)
		if data.Picking {
			path += "&pick=1"
		}
		return path
	}
	if number > 1 {
		data.Previous = pageURL(number - 1)
	}
	if number < data.Pages {
		data.Next = pageURL(number + 1)
	}

	page := m.views["mediaLibrary"].Content(r, data)
	page.Title = "Media"
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display uploaded images.", err)
	}
}
