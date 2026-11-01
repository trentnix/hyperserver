package blog

import (
	"strconv"
	"strings"
)

const slugConflictMessage = "This URL slug is already in use. Choose a different one, or leave it blank to generate one from the title."

// slugFromTitle uses lowercase ASCII letters and digits, separates other runs
// with hyphens, and caps the result at 120 bytes. An empty result needs a custom slug.
func slugFromTitle(title string) string {
	slug := make([]byte, 0, 120)
	for _, ch := range title {
		if len(slug) == 120 {
			break
		}
		if ch >= 'A' && ch <= 'Z' {
			ch += 'a' - 'A'
		}
		if ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' {
			slug = append(slug, byte(ch))
		} else if len(slug) > 0 && slug[len(slug)-1] != '-' {
			slug = append(slug, '-')
		}
	}
	return strings.TrimRight(string(slug), "-")
}

// numberedSlug appends a collision suffix to a valid base without exceeding 120 bytes.
func numberedSlug(base string, number int) string {
	suffix := "-" + strconv.Itoa(number)
	if len(base)+len(suffix) > 120 {
		base = base[:120-len(suffix)]
	}
	return strings.TrimRight(base, "-") + suffix
}

// validSlug accepts lowercase ASCII letters and digits separated by single hyphens,
// with no leading or trailing hyphen and a maximum length of 120 bytes.
func validSlug(slug string) bool {
	if slug == "" || len(slug) > 120 || strings.HasPrefix(slug, "-") || strings.HasSuffix(slug, "-") || strings.Contains(slug, "--") {
		return false
	}
	for _, ch := range slug {
		if !(ch >= 'a' && ch <= 'z') && !(ch >= '0' && ch <= '9') && ch != '-' {
			return false
		}
	}
	return true
}
