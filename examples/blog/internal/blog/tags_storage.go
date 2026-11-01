package blog

import (
	"context"
	"slices"
	"strings"
)

// ListTags returns unique labels from current posts and pages in alphabetical order.
func (s *sqliteRepository) ListTags(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT tags FROM blog_posts WHERE tags<>'' UNION SELECT tags FROM blog_pages WHERE tags<>''")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := make(map[string]bool)
	var tags []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		for _, tag := range strings.Split(value, ", ") {
			if !seen[tag] {
				seen[tag] = true
				tags = append(tags, tag)
			}
		}
	}
	slices.Sort(tags)
	return tags, rows.Err()
}
