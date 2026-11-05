package blog

import (
	"bytes"

	"golang.org/x/text/cases"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// contentMatcher owns reusable normalization buffers. It must not be shared
// between searches. Normalized text is valid until the next normalize call.
type contentMatcher struct {
	query     string
	normalize func(string) []byte
}

func newContentMatcher(query string) *contentMatcher {
	fold := cases.Fold()
	var source, folded, normalized []byte
	return &contentMatcher{
		query: norm.NFC.String(fold.String(query)),
		normalize: func(text string) []byte {
			source = append(source[:0], text...)
			// Full case folding accepts arbitrary bytes. Append supplies EOF and
			// grows the destination as needed, so neither short-buffer error applies.
			folded, _, _ = transform.Append(&fold, folded[:0], source)
			normalized = norm.NFC.Append(normalized[:0], folded...)
			return normalized
		},
	}
}

// rank prefers the first matching field. A negative rank excludes the record.
func (m *contentMatcher) rank(texts ...string) int {
	for rank, text := range texts {
		if bytes.Contains(m.normalize(text), []byte(m.query)) {
			return rank
		}
	}
	return -1
}
