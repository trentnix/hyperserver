package blog

import (
	"cmp"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/util"
	"golang.org/x/text/cases"
)

const mediaPageSize = 24

// SizeLabel formats total stored bytes for the media cards, not the download size.
func (f mediaFile) SizeLabel() string {
	switch {
	case f.Size >= 1024*1024:
		return fmt.Sprintf("%.1f MiB", float64(f.Size)/(1024*1024))
	case f.Size >= 1024:
		return fmt.Sprintf("%.1f KiB", float64(f.Size)/1024)
	default:
		return fmt.Sprintf("%d B", f.Size)
	}
}

// mediaDetailPage pairs an upload with its current and historical references.
type mediaDetailPage struct {
	ID    string
	Usage mediaUsage
	mediaMetadata
	Error string
}

// mediaDetail shows why an image is retained without changing its visibility.
func (m *module) mediaDetail(w http.ResponseWriter, r *http.Request) {
	m.renderMediaDetail(w, r, nil, "", http.StatusOK)
}

// renderMediaDetail preserves invalid input without losing the image or usage information.
func (m *module) renderMediaDetail(w http.ResponseWriter, r *http.Request, submitted *mediaMetadata, message string, status int) {
	id := r.PathValue("id")
	image, err := m.images.Open(r.Context(), id)
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	image.Content.Close()
	metadata, err := m.mediaMetadata.GetMediaMetadata(r.Context(), id)
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	if submitted != nil {
		metadata.Label, metadata.Version = submitted.Label, submitted.Version
	}
	used, err := m.mediaReferences.ReferencedImages(r.Context())
	if err != nil {
		m.storageError(w, r, err)
		return
	}

	page := m.views["mediaDetail"].Content(r, mediaDetailPage{ID: id, Usage: used[id], mediaMetadata: metadata, Error: message})
	page.Title, page.ResponseStatusCode = "Image details", status
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display this image.", err)
	}
}

// renameMedia changes only the administrator-facing label, never the image URL or alt text.
func (m *module) renameMedia(w http.ResponseWriter, r *http.Request) {
	if err := form.Parse(r); err != nil {
		m.renderMediaDetail(w, r, nil, err.Message, err.StatusCode)
		return
	}
	label := strings.TrimSpace(r.PostForm.Get("label"))
	version, err := strconv.ParseInt(r.PostForm.Get("version"), 10, 64)
	if err != nil || version < 0 {
		m.renderMediaDetail(w, r, nil, "Reload the image details before saving.", http.StatusBadRequest)
		return
	}
	submitted := mediaMetadata{Label: label, Version: version}
	if !utf8.ValidString(label) || utf8.RuneCountInString(label) > 240 || strings.ContainsFunc(label, unicode.IsControl) {
		m.renderMediaDetail(w, r, &submitted, "Use at most 240 characters without control characters.", http.StatusUnprocessableEntity)
		return
	}

	m.mediaMu.RLock()
	defer m.mediaMu.RUnlock()
	id := r.PathValue("id")
	image, err := m.images.Open(r.Context(), id)
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	image.Content.Close()
	err = m.mediaMetadata.RenameMedia(r.Context(), id, label, version)
	if errors.Is(err, errEditConflict) {
		m.renderMediaDetail(w, r, &submitted, "This image label changed in another window. Reload before saving.", http.StatusConflict)
	} else if err != nil {
		m.storageError(w, r, err)
	} else {
		util.RedirectToURL(w, r, "/media/"+id)
	}
}

// mediaLibraryItem adds a display name without changing filesystem identity.
type mediaLibraryItem struct {
	mediaFile
	Name string
}

// mediaLibraryPage describes a bounded page of existing uploads, not their public visibility.
type mediaLibraryPage struct {
	Files          []mediaLibraryItem
	Page, Pages    int
	Previous, Next string
	Picking        bool
	Query          mediaQuery
	Total          int
	ClearURL       string
}

// mediaQuery uses literal text, not SQL wildcards or a search expression language.
type mediaQuery struct{ Search, Usage string }

// Active distinguishes an empty library from filters that matched nothing.
func (q mediaQuery) Active() bool { return q.Search != "" || q.Usage != "" }

// matches includes saved-version titles so retained images can be located by their old use.
func (q mediaQuery) matches(file mediaFile, metadata mediaMetadata, usage mediaUsage) bool {
	switch q.Usage {
	case "current":
		if !usage.Current {
			return false
		}
	case "history":
		if usage.Current || len(usage.History) == 0 {
			return false
		}
	case "unused":
		if usage.Current || len(usage.History) > 0 {
			return false
		}
	}
	if q.Search == "" {
		return true
	}
	fold := cases.Fold()
	query := fold.String(q.Search)
	contains := func(value string) bool { return strings.Contains(fold.String(value), query) }
	if contains(file.ID) || contains(metadata.Label) || contains(metadata.Filename) {
		return true
	}
	for _, ref := range usage.Content {
		if contains(ref.Title) {
			return true
		}
	}
	for _, ref := range usage.History {
		if contains(ref.Title) {
			return true
		}
	}
	return false
}

// mediaLibrary lets only the administrator browse uploads, including private images.
// Selecting a file does not reserve it. Normal save validation rechecks its existence.
func (m *module) mediaLibrary(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()
	query := mediaQuery{Search: strings.TrimSpace(values.Get("q")), Usage: values.Get("usage")}
	if !utf8.ValidString(query.Search) || utf8.RuneCountInString(query.Search) > 240 ||
		!slices.Contains([]string{"", "current", "history", "unused"}, query.Usage) {
		http.Error(w, "Invalid media filters.", http.StatusBadRequest)
		return
	}
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
	metadata, err := m.mediaMetadata.ListMediaMetadata(r.Context())
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	var used map[string]mediaUsage
	if query.Search != "" || query.Usage != "" {
		used, err = m.mediaReferences.ReferencedImages(r.Context())
		if err != nil {
			m.storageError(w, r, err)
			return
		}
	}
	slices.SortFunc(files, func(a, b mediaFile) int {
		if order := b.Modified.Compare(a.Modified); order != 0 {
			return order
		}
		return cmp.Compare(a.ID, b.ID)
	})
	var matches []mediaLibraryItem
	for _, file := range files {
		name := metadata[file.ID]
		if !query.matches(file, name, used[file.ID]) {
			continue
		}
		label := name.Label
		if label == "" {
			label = name.Filename
		}
		if label == "" {
			label = file.ID
		}
		matches = append(matches, mediaLibraryItem{mediaFile: file, Name: label})
	}
	data := mediaLibraryPage{Page: number, Pages: max(1, (len(matches)+mediaPageSize-1)/mediaPageSize), Picking: values.Get("pick") == "1", Query: query, Total: len(matches), ClearURL: "/media"}
	if data.Picking {
		data.ClearURL += "?pick=1"
	}
	if number > data.Pages {
		m.storageError(w, r, errNotFound)
		return
	}
	start := (number - 1) * mediaPageSize
	data.Files = matches[start:min(start+mediaPageSize, len(matches))]
	pageURL := func(number int) string {
		params := url.Values{"page": {strconv.Itoa(number)}}
		if data.Picking {
			params.Set("pick", "1")
		}
		if query.Search != "" {
			params.Set("q", query.Search)
		}
		if query.Usage != "" {
			params.Set("usage", query.Usage)
		}
		return "/media?" + params.Encode()
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
