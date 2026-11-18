package blog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

type seriesContent struct {
	Title string   `json:"title"`
	Posts []string `json:"posts"`
}

type seriesEditorData struct {
	PostID, Selection string
	Version           int64
	Baseline          string
	Value             *Series
	Picker            postPickerData
	CanEdit           bool
}

// The baseline detects author edits independently of changes in shared storage.
// It is a change hint, not an authorization or concurrency token.
func seriesBaseline(id, source string) string {
	var value seriesContent
	if id != "" {
		if err := decodeComponentJSON(source, &value); err != nil {
			return ""
		}
		value.Title = strings.TrimSpace(value.Title)
	}
	sum := sha256.Sum256([]byte(id + "\x00" + componentJSON(value)))
	return hex.EncodeToString(sum[:])
}

// seriesUpdate accompanies a post save. It never enters the post Markdown.
type seriesUpdate struct {
	Series   Series
	Posts    []string
	AuthorID string
}

func seriesSelection(value *Series) seriesContent {
	content := seriesContent{Title: value.Title, Posts: []string{}}
	for _, post := range value.Posts {
		content.Posts = append(content.Posts, post.ID)
	}
	return content
}

func (m *module) canEditSeries(ctx context.Context, value *Series) bool {
	account := user.GetUserFromContext(ctx)
	permission, err := m.access.Role(ctx, account)
	if err != nil || !permission.valid() {
		return false
	}
	if permission == roleAdmin {
		return true
	}
	return account != nil && value.AuthorID == account.ID && !slices.ContainsFunc(value.Posts, func(post Post) bool { return post.AuthorID != account.ID })
}

func (m *module) loadSeriesEditor(r *http.Request, postID, selection string) (*seriesEditorData, error) {
	data := &seriesEditorData{PostID: postID, CanEdit: true, Picker: postPickerData{ID: "series-content", Name: "series", TitleLabel: "Series name", MaxPosts: maxSeriesPosts}}
	data.Baseline = seriesBaseline("", "")
	var current *Series
	var err error
	if postID != "" {
		current, err = m.series.SeriesForPost(r.Context(), postID, false)
		if err != nil {
			return nil, err
		}
	}
	data.Value = current
	switch selection {
	case "new":
		if current != nil {
			return nil, errSeriesMembership
		}
		data.Value = &Series{}
	case "":
	default:
		if !validRelatedID(selection) {
			return nil, contentError("Choose a valid series.")
		}
		if current != nil && current.ID != selection {
			return nil, errSeriesMembership
		}
		data.Value, err = m.series.GetSeries(r.Context(), selection)
		if err != nil {
			return nil, err
		}
	}
	if data.Value == nil {
		return data, nil
	}
	data.CanEdit = data.Value.ID == "" || m.canEditSeries(r.Context(), data.Value)
	if !data.CanEdit {
		if current == nil {
			return nil, errAccessDenied
		}
		return data, nil
	}
	data.Version = data.Value.Version
	data.Selection = data.Value.ID
	if data.Selection == "" {
		data.Selection = "new"
	}
	content := seriesSelection(data.Value)
	if current == nil {
		if len(content.Posts) >= maxSeriesPosts {
			return nil, contentError("This series already has 100 posts. Remove one before adding another.")
		}
		if postID != "" {
			content.Posts = append(content.Posts, postID)
		}
	}
	data.Picker.Source = componentJSON(content)
	data.Baseline = seriesBaseline(data.Selection, data.Picker.Source)
	return data, nil
}

// The read-only endpoint supplies series details and post choices to the post editor.
func (m *module) seriesEditor(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("choices") == "1" {
		m.seriesPostChoices(w, r)
		return
	}
	if r.URL.Query().Has("q") {
		m.searchSeries(w, r)
		return
	}
	data, err := m.loadSeriesEditor(r, r.PathValue("id"), r.URL.Query().Get("collection"))
	if err != nil {
		var invalid contentError
		if errors.Is(err, errAccessDenied) {
			http.Error(w, "You don't have permission to access this series.", http.StatusForbidden)
		} else if errors.As(err, &invalid) {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		} else {
			m.storageError(w, r, err)
		}
		return
	}
	if !data.CanEdit {
		http.Error(w, "This series is now managed by another author. Reload the post editor to view it.", http.StatusForbidden)
		return
	}
	writeJSON(w, r, http.StatusOK, struct {
		ID       string `json:"id"`
		Version  int64  `json:"version"`
		Source   string `json:"source"`
		Baseline string `json:"baseline"`
	}{data.Selection, data.Version, data.Picker.Source, data.Baseline})
}

func (m *module) searchSeries(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if !utf8.ValidString(query) || utf8.RuneCountInString(query) > 240 {
		http.Error(w, "Search must be at most 240 characters.", http.StatusBadRequest)
		return
	}
	scope := ""
	if m.requestRole(r) != roleAdmin {
		scope = user.GetUserFromContext(r.Context()).ID
	}
	ctx, cancel := context.WithTimeout(r.Context(), searchTimeout)
	defer cancel()
	choices, err := m.series.SearchSeries(ctx, query, scope)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			http.Error(w, "Series search is temporarily unavailable. Try again.", http.StatusServiceUnavailable)
		} else {
			m.storageError(w, r, err)
		}
		return
	}
	writeJSON(w, r, http.StatusOK, choices)
}

func readSeriesUpdate(r *http.Request) (*seriesUpdate, error) {
	id := r.PostForm.Get("seriesID")
	if baseline := r.PostForm.Get("seriesBaseline"); baseline != "" && baseline == seriesBaseline(id, r.PostForm.Get("series")) {
		return nil, nil
	}
	if id == "" {
		return nil, nil
	}
	value, err := parseSeriesContent(r.PostForm.Get("series"))
	if err != nil {
		return nil, writingFieldError{err, "series"}
	}
	version, err := strconv.ParseInt(r.PostForm.Get("seriesVersion"), 10, 64)
	if err != nil || id != "new" && (!validRelatedID(id) || version < 1) || id == "new" && version != 0 {
		return nil, writingFieldError{contentError("Reload the series settings before saving."), "series"}
	}
	if id == "new" {
		id = ""
	}
	return &seriesUpdate{Series: Series{ID: id, Title: value.Title, Version: version}, Posts: value.Posts}, nil
}

func (m *module) authorizeSeriesUpdate(ctx context.Context, update *seriesUpdate) (*seriesUpdate, error) {
	if update == nil {
		return nil, nil
	}
	copy := *update
	account := user.GetUserFromContext(ctx)
	permission, err := m.access.Role(ctx, account)
	if err != nil {
		return nil, err
	}
	if !permission.valid() {
		return nil, errAccessDenied
	}
	copy.Series.Title = strings.TrimSpace(copy.Series.Title)
	if err := validateSeriesContent(seriesContent{Title: copy.Series.Title, Posts: copy.Posts}); err != nil {
		return nil, writingFieldError{err, "series"}
	}
	if copy.Series.ID == "" {
		if copy.Series.Version != 0 {
			return nil, writingFieldError{errEditConflict, "series"}
		}
		copy.Series.AuthorID = account.ID
	} else if !validRelatedID(copy.Series.ID) || copy.Series.Version < 1 {
		return nil, writingFieldError{errEditConflict, "series"}
	}
	copy.AuthorID = ""
	if permission != roleAdmin {
		copy.AuthorID = account.ID
	}
	return &copy, nil
}

func parseSeriesContent(source string) (seriesContent, error) {
	var value seriesContent
	if err := decodeComponentJSON(source, &value); err != nil || !utf8.ValidString(source) {
		return value, contentError("Unable to read the series. Check its source before saving.")
	}
	value.Title = strings.TrimSpace(value.Title)
	return value, validateSeriesContent(value)
}

func validateSeriesContent(value seriesContent) error {
	if value.Title == "" || !utf8.ValidString(value.Title) || utf8.RuneCountInString(value.Title) > 240 {
		return contentError("Enter a series name of at most 240 characters.")
	}
	if value.Posts == nil || len(value.Posts) > maxSeriesPosts {
		return contentError("Choose up to 100 posts for the series.")
	}
	seen := make(map[string]bool)
	for _, id := range value.Posts {
		if !validRelatedID(id) || seen[id] {
			return contentError("Choose each post only once using a valid post ID.")
		}
		seen[id] = true
	}
	return nil
}

func (m *module) seriesPostChoices(w http.ResponseWriter, r *http.Request) {
	query, collection := strings.TrimSpace(r.URL.Query().Get("q")), r.URL.Query().Get("collection")
	ids := strings.Split(r.URL.Query().Get("ids"), ",")
	if r.URL.Query().Get("ids") == "" {
		ids = nil
	}
	if !utf8.ValidString(query) || utf8.RuneCountInString(query) > 240 || len(ids) > maxSeriesPosts || collection != "" && !validRelatedID(collection) {
		http.Error(w, "Invalid post search.", http.StatusBadRequest)
		return
	}
	for _, id := range ids {
		if !validRelatedID(id) {
			http.Error(w, "Invalid post selection.", http.StatusBadRequest)
			return
		}
	}
	scope := ""
	if m.requestRole(r) != roleAdmin {
		scope = user.GetUserFromContext(r.Context()).ID
	}
	posts, err := m.series.AvailableSeriesPosts(r.Context(), collection, scope)
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	result := struct {
		Posts    []relatedChoice `json:"posts"`
		Selected []relatedChoice `json:"selected"`
	}{Posts: []relatedChoice{}, Selected: []relatedChoice{}}
	match := newContentMatcher(query)
	for _, post := range posts {
		if !m.canEditPost(r.Context(), post) {
			continue
		}
		choice := relatedChoice{ID: post.ID, Title: post.Title}
		if post.Draft {
			choice.State = "Draft"
		} else if post.Scheduled(m.currentTime()) {
			choice.State = "Scheduled"
		}
		if slices.Contains(ids, post.ID) {
			result.Selected = append(result.Selected, choice)
		} else if query != "" && match.rank(post.Title, "", "") >= 0 && len(result.Posts) < 30 {
			result.Posts = append(result.Posts, choice)
		}
	}
	writeJSON(w, r, http.StatusOK, result)
}

type seriesNavigation struct {
	Series
	CurrentID      string
	Position       int
	Previous, Next *Post
}

func (m *module) seriesNavigation(ctx context.Context, postID string) (*seriesNavigation, error) {
	if postID == "" {
		return nil, nil
	}
	value, err := m.series.SeriesForPost(ctx, postID, true)
	if err != nil || value == nil {
		return nil, err
	}
	for index, post := range value.Posts {
		if post.ID == postID {
			navigation := &seriesNavigation{Series: *value, CurrentID: postID, Position: index + 1}
			if index > 0 {
				navigation.Previous = &value.Posts[index-1]
			}
			if index+1 < len(value.Posts) {
				navigation.Next = &value.Posts[index+1]
			}
			return navigation, nil
		}
	}
	return nil, nil
}
