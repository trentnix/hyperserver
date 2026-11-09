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
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/util"
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
	Error          string
	ErrorFields    string
	TagSuggestions []string
}

// mediaDetail shows why an image is retained without changing its visibility.
func (m *module) mediaDetail(w http.ResponseWriter, r *http.Request) {
	m.renderMediaDetail(w, r, nil, "", http.StatusOK)
}

// renderMediaDetail preserves invalid input without losing the image or usage information.
func (m *module) renderMediaDetail(w http.ResponseWriter, r *http.Request, submitted *mediaMetadata, message string, status int, fields ...string) {
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
		metadata.Label, metadata.Tags, metadata.Version = submitted.Label, submitted.Tags, submitted.Version
	}
	used, err := m.mediaReferences.ReferencedImages(r.Context(), id)
	if err != nil {
		m.storageError(w, r, err)
		return
	}

	page := m.views["mediaDetail"].Content(r, mediaDetailPage{ID: id, Usage: used[id], mediaMetadata: metadata, Error: message, ErrorFields: strings.Join(fields, " "), TagSuggestions: m.mediaTagSuggestions(r)})
	page.Title, page.ResponseStatusCode = "Image details", status
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display this image.", err)
	}
}

// saveMediaMetadata changes search metadata, never the image URL, visibility, or alt text.
func (m *module) saveMediaMetadata(w http.ResponseWriter, r *http.Request) {
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
	submitted := mediaMetadata{Label: label, Tags: r.PostForm.Get("tags"), Version: version}
	if !utf8.ValidString(label) || utf8.RuneCountInString(label) > 240 || strings.ContainsFunc(label, unicode.IsControl) {
		m.renderMediaDetail(w, r, &submitted, "Use at most 240 characters without control characters.", http.StatusUnprocessableEntity, "label")
		return
	}
	tags, err := normalizeTags(submitted.Tags)
	if err != nil {
		m.renderMediaDetail(w, r, &submitted, err.Error(), http.StatusUnprocessableEntity, "tags")
		return
	}
	submitted.Tags = tags

	m.mediaMu.RLock()
	defer m.mediaMu.RUnlock()
	id := r.PathValue("id")
	image, err := m.images.Open(r.Context(), id)
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	image.Content.Close()
	err = m.mediaMetadata.SaveMediaMetadata(r.Context(), id, submitted)
	if errors.Is(err, errEditConflict) {
		m.renderMediaDetail(w, r, &submitted, "These image details changed in another window. Reload before saving.", http.StatusConflict)
	} else if err != nil {
		m.storageError(w, r, err)
	} else {
		util.RedirectToURL(w, r, "/media/"+id)
	}
}

// mediaTagSuggestions excludes writing tags and metadata left behind by deleted uploads.
// Suggestions are optional, so a lookup failure must not prevent editing.
func (m *module) mediaTagSuggestions(r *http.Request) []string {
	files, err := m.media.List(r.Context())
	if err != nil {
		logger.LogRequestError(r, err)
		return nil
	}
	metadata, err := m.mediaMetadata.ListMediaMetadata(r.Context())
	if err != nil {
		logger.LogRequestError(r, err)
		return nil
	}
	seen := make(map[string]bool)
	var tags []string
	for _, file := range files {
		for _, tag := range strings.Split(metadata[file.ID].Tags, ", ") {
			if tag != "" && !seen[tag] {
				seen[tag] = true
				tags = append(tags, tag)
			}
		}
	}
	slices.Sort(tags)
	return tags
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

// mediaMatcher belongs to one request and reuses Unicode normalization buffers.
type mediaMatcher struct {
	query mediaQuery
	text  *contentMatcher
}

func (q mediaQuery) matcher() mediaMatcher {
	return mediaMatcher{query: q, text: newContentMatcher(q.Search)}
}

// rank prefers names, then tags, then current or saved-version titles.
// A negative rank excludes images that do not match the text or usage filter.
func (m mediaMatcher) rank(file mediaFile, metadata mediaMetadata, usage mediaUsage) int {
	switch m.query.Usage {
	case "current":
		if !usage.Current {
			return -1
		}
	case "history":
		if usage.Current || len(usage.History) == 0 {
			return -1
		}
	case "unused":
		if usage.Current || len(usage.History) > 0 {
			return -1
		}
	}
	if m.query.Search == "" {
		return 0
	}
	if m.text.rank(file.ID, metadata.Label, metadata.Filename) >= 0 {
		return 0
	}
	if m.text.rank(metadata.Tags) >= 0 {
		return 1
	}
	for _, ref := range usage.Content {
		if m.text.rank(ref.Title) >= 0 {
			return 2
		}
	}
	for _, ref := range usage.History {
		if m.text.rank(ref.Title) >= 0 {
			return 2
		}
	}
	return -1
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
	var metadata map[string]mediaMetadata
	if query.Search != "" {
		metadata, err = m.mediaMetadata.ListMediaMetadata(r.Context())
		if err != nil {
			m.storageError(w, r, err)
			return
		}
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
	matches := files
	if query.Active() {
		match := query.matcher()
		var ranked [3][]mediaFile
		for _, file := range files {
			rank := match.rank(file, metadata[file.ID], used[file.ID])
			if rank >= 0 {
				ranked[rank] = append(ranked[rank], file)
			}
		}
		matches = nil
		for _, group := range ranked {
			matches = append(matches, group...)
		}
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
	visible := matches[start:min(start+mediaPageSize, len(matches))]
	if query.Search == "" && len(visible) > 0 {
		ids := make([]string, 0, len(visible))
		for _, file := range visible {
			ids = append(ids, file.ID)
		}
		metadata, err = m.mediaMetadata.ListMediaMetadata(r.Context(), ids...)
		if err != nil {
			m.storageError(w, r, err)
			return
		}
	}
	for _, file := range visible {
		name := metadata[file.ID]
		label := name.Label
		if label == "" {
			label = name.Filename
		}
		if label == "" {
			label = file.ID
		}
		data.Files = append(data.Files, mediaLibraryItem{mediaFile: file, Name: label})
	}
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
