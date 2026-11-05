package blog

import (
	"context"
	"strings"
)

// SearchPublic scans only visible current content in one database snapshot.
// SQLite orders metadata without loading bodies into temporary sort storage.
// Each rank retains only its first searchResultLimit matches.
func (s *sqliteRepository) SearchPublic(ctx context.Context, query string) (searchResults, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return searchResults{}, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return searchResults{}, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, publishedPages+`
SELECT 'post' AS kind, slug, title, tags, summary, published AS sort_time, id, version
FROM blog_posts WHERE draft=0 AND published<=?
UNION ALL
SELECT 'page', slug, title, tags, summary, updated / 1000000000, id, version
FROM blog_pages WHERE id IN (SELECT id FROM visible)
ORDER BY sort_time DESC, kind, id`, s.now().Unix())
	if err != nil {
		return searchResults{}, err
	}
	defer rows.Close()
	match := newContentMatcher(query)
	var ranks [4][]searchResult
	var results searchResults
	var item searchResult
	var tags, id string
	var sortTime, version int64
	for rows.Next() {
		if err := rows.Scan(&item.Kind, &item.Slug, &item.Title, &tags, &item.Summary, &sortTime, &id, &version); err != nil {
			return searchResults{}, err
		}
		rank := match.rank(item.Title, tags, item.Summary)
		if rank < 0 {
			found, err := s.matchBody(ctx, tx, match, item.Kind, id, version)
			if err != nil {
				return searchResults{}, err
			}
			if found {
				rank = 3
			}
		}
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
