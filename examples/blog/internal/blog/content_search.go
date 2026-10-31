package blog

import (
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// contentMatcher ranks literal, case-insensitive matches: title, tags, summary, then
// Markdown. A negative rank excludes the record. Each search owns its case folder.
func contentMatcher(query string) func(title, tags, summary, markdown string) int {
	fold := cases.Fold()
	query = norm.NFC.String(fold.String(query))
	return func(title, tags, summary, markdown string) int {
		for rank, text := range []string{title, tags, summary, markdown} {
			if strings.Contains(norm.NFC.String(fold.String(text)), query) {
				return rank
			}
		}
		return -1
	}
}
