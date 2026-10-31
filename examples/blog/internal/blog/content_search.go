package blog

import (
	"strings"

	"golang.org/x/text/cases"
)

// contentMatcher ranks literal, case-insensitive matches: title, summary, then
// Markdown. A negative rank excludes the record. Each search owns its case folder.
func contentMatcher(query string) func(title, summary, markdown string) int {
	fold := cases.Fold()
	query = fold.String(query)
	return func(title, summary, markdown string) int {
		for rank, text := range []string{title, summary, markdown} {
			if strings.Contains(fold.String(text), query) {
				return rank
			}
		}
		return -1
	}
}
