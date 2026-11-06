package blog

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/trentnix/hyperserver/pkg/services/logger"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// tagRepository supplies editor suggestions from current content, including drafts.
// Suggestions are administrator-only and exclude tags found solely in history.
type tagRepository interface {
	ListTags(context.Context) ([]string, error)
}

// tagSuggestions keeps optional suggestions from blocking writing or hiding a save error.
func (m *module) tagSuggestions(r *http.Request) []string {
	if m.requestRole(r) == rolePoster {
		return nil
	}
	tags, err := m.tags.ListTags(r.Context())
	if err != nil {
		logger.LogRequestError(r, err)
		return nil
	}
	return tags
}

// normalizeTags stores a small, sorted, comma-separated set. Unicode case folding
// and normalization prevent equivalent labels from becoming separate tags.
func normalizeTags(value string) (string, error) {
	if !utf8.ValidString(value) || len(value) > 4096 {
		return "", contentError("Keep tags under 4 KiB of valid text.")
	}
	fold := cases.Fold()
	seen := make(map[string]bool)
	var tags []string
	for _, tag := range strings.Split(value, ",") {
		if strings.ContainsFunc(tag, unicode.IsControl) {
			return "", contentError("Tags cannot contain control characters or line breaks.")
		}
		tag = norm.NFC.String(fold.String(strings.Join(strings.Fields(tag), " ")))
		if tag == "" || seen[tag] {
			continue
		}
		if utf8.RuneCountInString(tag) > 50 {
			return "", contentError("Keep each tag to 50 characters or fewer.")
		}
		seen[tag] = true
		tags = append(tags, tag)
		if len(tags) > 20 {
			return "", contentError("Use at most 20 tags.")
		}
	}
	slices.Sort(tags)
	return strings.Join(tags, ", "), nil
}
