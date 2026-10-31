package blog

import (
	"context"
	"strings"
)

// SearchPublic scans only visible current content in one database snapshot.
// Each rank retains a bounded prefix. Later title matches can therefore displace
// earlier body matches without retaining every matching post or page in memory.
func (s *sqliteRepository) SearchPublic(ctx context.Context, query string) (searchResults, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return searchResults{}, nil
	}
	rows, err := s.db.QueryContext(ctx, publishedPages+`
SELECT 'post' AS kind, slug, title, summary, markdown, published AS sort_time, id
FROM blog_posts WHERE draft=0 AND published<=?
UNION ALL
SELECT 'page', slug, title, summary, markdown, updated / 1000000000, id
FROM blog_pages WHERE id IN (SELECT id FROM visible)
ORDER BY sort_time DESC, kind, id`, s.now().Unix())
	if err != nil {
		return searchResults{}, err
	}
	defer rows.Close()
	match := contentMatcher(query)
	var ranks [3][]searchResult
	var results searchResults
	for rows.Next() {
		var item searchResult
		var markdown, id string
		var sortTime int64
		if err := rows.Scan(&item.Kind, &item.Slug, &item.Title, &item.Summary, &markdown, &sortTime, &id); err != nil {
			return searchResults{}, err
		}
		rank := match(item.Title, item.Summary, markdown)
		if rank < 0 {
			continue
		}
		results.Total++
		if len(ranks[rank]) < searchResultLimit {
			item.Summary = searchSummary(item.Summary)
			ranks[rank] = append(ranks[rank], item)
		}
	}
	if err := rows.Err(); err != nil {
		return searchResults{}, err
	}
	for _, items := range ranks {
		remaining := searchResultLimit - len(results.Items)
		results.Items = append(results.Items, items[:min(remaining, len(items))]...)
	}
	return results, nil
}

// searchSummary keeps result descriptions short without cutting a UTF-8 character.
func searchSummary(text string) string {
	runes := []rune(strings.Join(strings.Fields(text), " "))
	if len(runes) > 200 {
		return string(runes[:200]) + "…"
	}
	return string(runes)
}
