package blog

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"slices"
	"strings"
)

const (
	maxSeriesPosts    = 100
	seriesSearchLimit = 20
)

var errSeriesMembership = contentError("A selected post already belongs to another series. Remove it from that series first.")

// A series is shared metadata. Its order and membership never enter post Markdown.
type Series struct {
	ID, Title, AuthorID string
	Version             int64
	Posts               []Post
}

type seriesRepository interface {
	SeriesForPost(context.Context, string, bool) (*Series, error)
	GetSeries(context.Context, string) (*Series, error)
	SearchSeries(context.Context, string, string) ([]seriesChoice, error)
	AvailableSeriesPosts(context.Context, string, string) ([]Post, error)
}

func (s *sqliteRepository) AvailableSeriesPosts(ctx context.Context, collection, authorID string) ([]Post, error) {
	return s.listPosts(ctx, `SELECT p.id,p.slug,p.title,'','',p.published,p.draft,'',p.version,'',p.author_id
FROM blog_post_current p LEFT JOIN blog_collection_content c ON c.post_id=p.id
WHERE (?='' OR p.author_id=?) AND (c.collection_id IS NULL OR c.collection_id=?)
ORDER BY p.draft DESC,p.published DESC,p.rowid DESC`, authorID, authorID, collection)
}

// seriesChoice contains only the metadata needed to choose a manageable series.
type seriesChoice struct {
	ID    string   `json:"id"`
	Title string   `json:"title"`
	Count int      `json:"count"`
	Posts []string `json:"posts"`
}

// SearchSeries matches series names and current member writing. Name matches
// precede member title, tag, summary, and body matches. Results are bounded.
func (s *sqliteRepository) SearchSeries(ctx context.Context, query, authorID string) ([]seriesChoice, error) {
	result := []seriesChoice{}
	query = strings.TrimSpace(query)
	if query == "" {
		return result, nil
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT c.id,c.title,coalesce(p.id,''),coalesce(p.title,''),coalesce(p.tags,''),coalesce(p.summary,''),coalesce(p.version,0)
FROM blog_collections c
LEFT JOIN blog_collection_content member ON member.collection_id=c.id
LEFT JOIN blog_post_current p ON p.id=member.post_id
WHERE (?='' OR c.author_id=?)
AND (?='' OR NOT EXISTS (SELECT 1 FROM blog_collection_content other JOIN blog_current_posts owned ON owned.id=other.post_id WHERE other.collection_id=c.id AND owned.author_id<>?))
AND (SELECT count(*) FROM blog_collection_content WHERE collection_id=c.id)<?
ORDER BY c.title COLLATE NOCASE,c.id,member.position`, authorID, authorID, authorID, authorID, maxSeriesPosts)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	match := newContentMatcher(query)
	var groups [5][]seriesChoice
	var current seriesChoice
	rank := -1
	flush := func() {
		if current.ID != "" && rank >= 0 && len(groups[rank]) < seriesSearchLimit {
			groups[rank] = append(groups[rank], current)
		}
	}
	for rows.Next() {
		var id, title string
		var post Post
		if err := rows.Scan(&id, &title, &post.ID, &post.Title, &post.Tags, &post.Summary, &post.Version); err != nil {
			return nil, err
		}
		if id != current.ID {
			flush()
			current = seriesChoice{ID: id, Title: title, Posts: []string{}}
			rank = match.rank(title)
		}
		if post.ID == "" {
			continue
		}
		current.Count++
		if rank == 0 {
			if len(current.Posts) < 2 {
				current.Posts = append(current.Posts, post.Title)
			}
			continue
		}
		postRank := match.rank(post.Title, post.Tags, post.Summary)
		if postRank < 0 {
			found, err := s.matchBody(ctx, tx, match, "post", post.ID, post.Version)
			if err != nil {
				return nil, err
			}
			if found {
				postRank = 3
			}
		}
		if postRank >= 0 {
			postRank++
			if rank < 0 || postRank < rank {
				rank = postRank
			}
			if len(current.Posts) < 2 {
				current.Posts = append(current.Posts, post.Title)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	flush()
	for _, group := range groups {
		for _, value := range group {
			result = append(result, value)
			if len(result) == seriesSearchLimit {
				return result, nil
			}
		}
	}
	return result, nil
}

func readSeries(ctx context.Context, tx *sql.Tx, id string, publicOnly bool, now int64) (*Series, error) {
	var value Series
	err := tx.QueryRowContext(ctx, `SELECT id,title,author_id,version FROM blog_collections WHERE id=?`, id).Scan(&value.ID, &value.Title, &value.AuthorID, &value.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT p.id,p.slug,p.title,'','',p.published,p.draft,'',p.version,'',p.author_id
FROM blog_collection_content c JOIN blog_post_current p ON p.id=c.post_id
WHERE c.collection_id=? AND (NOT ? OR (p.draft=0 AND p.published<=?)) ORDER BY c.position`, id, publicOnly, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		post, err := scanPost(rows)
		if err != nil {
			return nil, err
		}
		value.Posts = append(value.Posts, post)
	}
	return &value, rows.Err()
}

func (s *sqliteRepository) GetSeries(ctx context.Context, id string) (*Series, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	value, err := readSeries(ctx, tx, id, false, s.now().Unix())
	if err != nil {
		return nil, err
	}
	return value, tx.Commit()
}

func (s *sqliteRepository) SeriesForPost(ctx context.Context, postID string, publicOnly bool) (*Series, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(ctx, `SELECT collection_id FROM blog_collection_content WHERE post_id=?`, postID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	value, err := readSeries(ctx, tx, id, publicOnly, s.now().Unix())
	if err != nil {
		return nil, err
	}
	return value, tx.Commit()
}

// saveSeries checks the version, member ownership, and single-series membership
// inside the post save transaction before replacing the shared order.
func (s *sqliteRepository) saveSeries(ctx context.Context, tx *sql.Tx, value Series, ids []string, authorID string) (*Series, error) {
	if value.ID == "" {
		value.ID, value.Version = rand.Text(), 1
		if _, err := tx.ExecContext(ctx, `INSERT INTO blog_collections(id,title,author_id,version) VALUES(?,?,?,1)`, value.ID, value.Title, value.AuthorID); err != nil {
			return nil, err
		}
	} else {
		current, err := readSeries(ctx, tx, value.ID, false, s.now().Unix())
		if errors.Is(err, errNotFound) {
			return nil, errEditConflict
		}
		if err != nil {
			return nil, err
		}
		if authorID != "" && current.AuthorID != authorID {
			return nil, errAccessDenied
		}
		if current.Version != value.Version {
			return nil, errEditConflict
		}
		var previous []string
		for _, post := range current.Posts {
			if authorID != "" && post.AuthorID != authorID {
				return nil, errAccessDenied
			}
			previous = append(previous, post.ID)
		}
		if len(ids) > 0 && current.Title == value.Title && slices.Equal(previous, ids) {
			return current, nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE blog_collections SET title=?,version=version+1 WHERE id=?`, value.Title, value.ID); err != nil {
			return nil, err
		}
		value.AuthorID, value.Version = current.AuthorID, current.Version+1
	}
	for _, id := range ids {
		var owner, collection string
		err := tx.QueryRowContext(ctx, `SELECT p.author_id,coalesce(c.collection_id,'') FROM blog_current_posts p LEFT JOIN blog_collection_content c ON c.post_id=p.id WHERE p.id=?`, id).Scan(&owner, &collection)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, contentError("A selected post is unavailable. Remove it before saving the series.")
		}
		if err != nil {
			return nil, err
		}
		if authorID != "" && owner != authorID {
			return nil, errAccessDenied
		}
		if collection != "" && collection != value.ID {
			return nil, errSeriesMembership
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM blog_collection_content WHERE collection_id=?`, value.ID); err != nil {
		return nil, err
	}
	for position, id := range ids {
		if _, err := tx.ExecContext(ctx, `INSERT INTO blog_collection_content(collection_id,post_id,position) VALUES(?,?,?)`, value.ID, id, position); err != nil {
			return nil, err
		}
	}
	if len(ids) == 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM blog_collections WHERE id=?`, value.ID); err != nil {
			return nil, err
		}
		return nil, nil
	}
	saved, err := readSeries(ctx, tx, value.ID, false, s.now().Unix())
	if err != nil {
		return nil, err
	}
	return saved, nil
}
