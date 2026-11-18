package blog

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"slices"
	"time"

	"github.com/mattn/go-sqlite3"
)

const postColumns = "id, slug, title, summary, markdown, published, draft, preview_image, version, tags, author_id"

// scanPost decodes a row in postColumns order and maps missing rows to errNotFound.
func scanPost(row interface{ Scan(...any) error }) (Post, error) {
	var p Post
	var published int64
	err := row.Scan(&p.ID, &p.Slug, &p.Title, &p.Summary, &p.Markdown, &published, &p.Draft, &p.PreviewImage, &p.Version, &p.Tags, &p.AuthorID)
	if errors.Is(err, sql.ErrNoRows) {
		err = errNotFound
	}
	if published != 0 {
		p.Published = time.Unix(published, 0).UTC()
	}
	return p, err
}

// List returns complete posts newest first, using insertion order to break ties.
func (s *sqliteRepository) List(ctx context.Context) ([]Post, error) {
	return s.listPosts(ctx, "SELECT "+postColumns+" FROM blog_post_current WHERE draft=0 AND published<=? ORDER BY published DESC, rowid DESC", s.now().Unix())
}

// ListMetadata returns drafts newest-created first, then posts newest-published first.
func (s *sqliteRepository) ListMetadata(ctx context.Context) ([]Post, error) {
	return s.listPosts(ctx, "SELECT id, slug, title, '', '', published, draft, '', version, '', author_id FROM blog_post_current ORDER BY draft DESC, CASE WHEN draft=0 THEN published END DESC, rowid DESC")
}

// SearchPosts scans current content with Unicode case folding. Only matching
// metadata is returned. Equal ranks preserve the management list's order.
func (s *sqliteRepository) SearchPosts(ctx context.Context, query, authorID string) ([]Post, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	statement := "SELECT id, slug, title, summary, '', published, draft, '', version, tags, author_id FROM blog_post_current"
	var args []any
	if authorID != "" {
		statement += " WHERE author_id=?"
		args = append(args, authorID)
	}
	rows, err := tx.QueryContext(ctx, statement+" ORDER BY draft DESC, CASE WHEN draft=0 THEN published END DESC, rowid DESC", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	match := newContentMatcher(query)
	var matches [4][]Post
	for rows.Next() {
		post, err := scanPost(rows)
		if err != nil {
			return nil, err
		}
		rank := match.rank(post.Title, post.Tags, post.Summary)
		if rank < 0 {
			found, err := s.matchBody(ctx, tx, match, "post", post.ID, post.Version)
			if err != nil {
				return nil, err
			}
			if found {
				rank = 3
			}
		}
		if rank >= 0 {
			post.Summary, post.Markdown, post.PreviewImage = "", "", ""
			matches[rank] = append(matches[rank], post)
		}
	}
	var result []Post
	for _, group := range matches {
		result = append(result, group...)
	}
	return result, rows.Err()
}

// listPosts reads either complete public posts or administration metadata.
func (s *sqliteRepository) listPosts(ctx context.Context, query string, args ...any) ([]Post, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var posts []Post
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, err
		}
		posts = append(posts, p)
	}
	return posts, rows.Err()
}

// Latest fetches only the newest post, or returns errNotFound for an empty blog.
func (s *sqliteRepository) Latest(ctx context.Context) (Post, error) {
	return scanPost(s.db.QueryRowContext(ctx, "SELECT "+postColumns+" FROM blog_post_current WHERE draft=0 AND published<=? ORDER BY published DESC, rowid DESC LIMIT 1", s.now().Unix()))
}

// NextPublication bounds the lifetime of public caches without a background timer.
func (s *sqliteRepository) NextPublication(ctx context.Context) (time.Time, error) {
	var next sql.NullInt64
	err := s.db.QueryRowContext(ctx, "SELECT MIN(published) FROM blog_post_current WHERE draft=0 AND published>?", s.now().Unix()).Scan(&next)
	if err != nil || !next.Valid {
		return time.Time{}, err
	}
	return time.Unix(next.Int64, 0).UTC(), nil
}

// ListArchive returns metadata in the same order as List without loading Markdown.
func (s *sqliteRepository) ListArchive(ctx context.Context) ([]ArchiveEntry, error) {
	return s.listArchive(ctx, 0, 0)
}

// ListArchivePage bounds the metadata read in SQLite, including publication visibility.
func (s *sqliteRepository) ListArchivePage(ctx context.Context, limit, offset int) ([]ArchiveEntry, error) {
	if limit < 1 || offset < 0 {
		return nil, errors.New("archive limit must be positive and offset must be nonnegative")
	}
	return s.listArchive(ctx, limit, offset)
}

// ListRecent limits the database query, not just the rendered navigation.
func (s *sqliteRepository) ListRecent(ctx context.Context, limit int) ([]ArchiveEntry, error) {
	if limit < 1 {
		return nil, errors.New("recent post limit must be greater than zero")
	}
	return s.listArchive(ctx, limit, 0)
}

// listArchive reads link metadata. A zero limit selects the complete archive.
func (s *sqliteRepository) listArchive(ctx context.Context, limit, offset int) ([]ArchiveEntry, error) {
	query := "SELECT slug, title, published FROM blog_post_current WHERE draft=0 AND published<=? ORDER BY published DESC, rowid DESC"
	args := []any{s.now().Unix()}
	if limit > 0 {
		query += " LIMIT ? OFFSET ?"
		args = append(args, limit, offset)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []ArchiveEntry
	for rows.Next() {
		var entry ArchiveEntry
		var published int64
		if err := rows.Scan(&entry.Slug, &entry.Title, &published); err != nil {
			return nil, err
		}
		entry.Published = time.Unix(published, 0).UTC()
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// GetByID hides drafts and scheduled posts unless includeDrafts is true.
func (s *sqliteRepository) GetByID(ctx context.Context, id string, includeDrafts bool) (Post, error) {
	return scanPost(s.db.QueryRowContext(ctx, "SELECT "+postColumns+" FROM blog_post_current WHERE id=? AND (? OR (draft=0 AND published<=?))", id, includeDrafts, s.now().Unix()))
}

// GetBySlug hides drafts and scheduled posts unless includeDrafts is true.
func (s *sqliteRepository) GetBySlug(ctx context.Context, slug string, includeDrafts bool) (Post, error) {
	return scanPost(s.db.QueryRowContext(ctx, "SELECT "+postColumns+" FROM blog_post_current WHERE slug=? AND (? OR (draft=0 AND published<=?))", slug, includeDrafts, s.now().Unix()))
}

// Save atomically writes content and publication state, generating IDs for new posts.
// A future Published schedules publication. A saved draft can replace a past
// publication date with a new schedule. Other edits preserve past publication dates.
// Updates require the current Version and fail if the post no longer exists.
// The database enforces slug uniqueness and conflicts return errSlugTaken.
func (s *sqliteRepository) Save(ctx context.Context, p Post, refs []string) (Post, error) {
	return s.SaveWithSeries(ctx, p, refs, nil)
}

// SaveWithSeries commits writing, history, media references, and shared series
// changes together. A conflict in either edit rolls back the complete save.
func (s *sqliteRepository) SaveWithSeries(ctx context.Context, p Post, refs []string, update *seriesUpdate) (Post, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	saved, err := s.savePost(ctx, tx, p, refs)
	if err != nil {
		return p, err
	}
	if update != nil {
		ids := append([]string{}, update.Posts...)
		if p.ID == "" {
			ids = append(ids, saved.ID)
		}
		var current string
		err := tx.QueryRowContext(ctx, `SELECT collection_id FROM blog_collection_content WHERE post_id=?`, saved.ID).Scan(&current)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return p, err
		}
		if current != "" && current != update.Series.ID {
			return p, writingFieldError{errEditConflict, "series"}
		}
		if current == "" && !slices.Contains(ids, saved.ID) {
			return p, writingFieldError{contentError("Include this post when choosing its series."), "series"}
		}
		if len(ids) > maxSeriesPosts {
			return p, writingFieldError{contentError("Choose up to 100 posts for the series."), "series"}
		}
		if _, err := s.saveSeries(ctx, tx, update.Series, ids, update.AuthorID); err != nil {
			return p, writingFieldError{err, "series"}
		}
	}
	return saved, tx.Commit()
}

func (s *sqliteRepository) savePost(ctx context.Context, tx *sql.Tx, p Post, refs []string) (Post, error) {
	now := s.now().UTC().Truncate(time.Second)
	var publication int64
	if !p.Draft {
		publication = now.Unix()
		if p.Published.After(now) {
			publication = p.Published.Unix()
		}
	}
	if p.ID != "" {
		current, err := scanPost(tx.QueryRowContext(ctx, "SELECT "+postColumns+" FROM blog_post_current WHERE id=?", p.ID))
		if err != nil {
			return p, err
		}
		if p.Version != current.Version {
			return p, errEditConflict
		}
		// Editing content, including restoring a version, never transfers ownership.
		p.AuthorID = current.AuthorID
		candidate := p
		candidate.Published = time.Time{}
		if current.WasPublished(now) && !(current.Draft && publication > now.Unix()) {
			if publication > now.Unix() {
				return p, errAlreadyPublished
			}
			candidate.Published = current.Published
		} else if publication != 0 {
			candidate.Published = time.Unix(publication, 0).UTC()
		}
		if candidate == current {
			return current, nil
		}
	}

	var err error
	if p.ID == "" {
		p.ID = rand.Text()
		var published int64
		p.Published = time.Time{}
		if !p.Draft {
			p.Published = time.Unix(publication, 0).UTC()
			published = p.Published.Unix()
		}
		p.Version = 1
		_, err = tx.ExecContext(ctx, "INSERT INTO blog_current_posts (id,version_id,slug,published,draft,version,author_id) VALUES (?,0,?,?,?,?,?)", p.ID, p.Slug, published, p.Draft, p.Version, p.AuthorID)
	} else {
		var published int64
		err = tx.QueryRowContext(ctx, `UPDATE blog_current_posts SET slug=?, draft=?, version=version+1,
 published=CASE WHEN published>0 AND published<=? AND NOT (draft=1 AND ?>?) THEN published ELSE ? END WHERE id=? AND version=? RETURNING published,version`,
			p.Slug, p.Draft, now.Unix(), publication, now.Unix(), publication, p.ID, p.Version).Scan(&published, &p.Version)
		if errors.Is(err, sql.ErrNoRows) {
			err = editError(ctx, tx, "post", p.ID, errNotFound)
		}
		p.Published = time.Time{}
		if published != 0 {
			p.Published = time.Unix(published, 0).UTC()
		}
		if err == nil && publication > now.Unix() && p.WasPublished(now) {
			return p, errAlreadyPublished
		}
	}
	var constraint sqlite3.Error
	if errors.As(err, &constraint) && constraint.ExtendedCode == sqlite3.ErrConstraintUnique {
		err = errSlugTaken
	}
	if err == nil {
		var number int64
		number, err = saveContentVersion(ctx, tx, "post", p.ID, Revision{Title: p.Title, Summary: p.Summary, Markdown: p.Markdown, PreviewImage: p.PreviewImage, Tags: p.Tags}, refs)
		if err == nil {
			_, err = tx.ExecContext(ctx, "UPDATE blog_current_posts SET version_id=? WHERE id=?", number, p.ID)
		}
	}
	return p, err
}

// Delete removes one post or returns errNotFound. Images are retained because
// other posts can reference them.
func (s *sqliteRepository) Delete(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM blog_current_posts WHERE id=?", id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return errNotFound
	}
	return err
}
