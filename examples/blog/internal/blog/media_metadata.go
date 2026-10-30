package blog

import (
	"context"
	"database/sql"
	"errors"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

// mediaMetadata names an upload without changing its immutable image ID or URL.
type mediaMetadata struct {
	Filename, Label string
	Version         int64
}

// mediaMetadataRepository keeps display names separate from uploaded image bytes.
// Missing metadata describes a legacy upload, not a missing image.
type mediaMetadataRepository interface {
	ListMediaMetadata(context.Context) (map[string]mediaMetadata, error)
	GetMediaMetadata(context.Context, string) (mediaMetadata, error)
	RememberMedia(context.Context, string, string) error
	RenameMedia(context.Context, string, string, int64) error
}

// ListMediaMetadata supplies names for the filesystem-backed library. Records for
// deleted files are ignored and retain their labels if the same bytes are re-uploaded.
func (s *sqliteRepository) ListMediaMetadata(ctx context.Context) (map[string]mediaMetadata, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, filename, label, version FROM blog_media")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	metadata := make(map[string]mediaMetadata)
	for rows.Next() {
		var id string
		var value mediaMetadata
		if err := rows.Scan(&id, &value.Filename, &value.Label, &value.Version); err != nil {
			return nil, err
		}
		metadata[id] = value
	}
	return metadata, rows.Err()
}

// GetMediaMetadata returns an empty value for uploads created before naming was available.
func (s *sqliteRepository) GetMediaMetadata(ctx context.Context, id string) (mediaMetadata, error) {
	var value mediaMetadata
	err := s.db.QueryRowContext(ctx, "SELECT filename, label, version FROM blog_media WHERE id=?", id).Scan(&value.Filename, &value.Label, &value.Version)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return value, err
}

// RememberMedia retains the first filename and never overwrites an administrator's label.
func (s *sqliteRepository) RememberMedia(ctx context.Context, id, filename string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO blog_media(id, filename, label, version) VALUES (?, ?, '', 1)
ON CONFLICT(id) DO UPDATE SET filename=excluded.filename, version=blog_media.version+1
WHERE blog_media.filename='' AND excluded.filename<>''`, id, filename)
	return err
}

// RenameMedia rejects stale edits. A version of zero creates metadata for a legacy upload.
func (s *sqliteRepository) RenameMedia(ctx context.Context, id, label string, version int64) error {
	var result sql.Result
	var err error
	if version == 0 {
		result, err = s.db.ExecContext(ctx, "INSERT INTO blog_media(id, filename, label, version) VALUES (?, '', ?, 1) ON CONFLICT(id) DO NOTHING", id, label)
	} else {
		result, err = s.db.ExecContext(ctx, "UPDATE blog_media SET label=?, version=version+1 WHERE id=? AND version=?", label, id, version)
	}
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		err = errEditConflict
	}
	return err
}

// uploadFilename discards client path components and control characters. It is
// display metadata only and is never used to construct a filesystem path.
func uploadFilename(name string) string {
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(name, "")))
	if name == "." || name == "/" {
		return ""
	}
	return string([]rune(name)[:min(utf8.RuneCountInString(name), 240)])
}
