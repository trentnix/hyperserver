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

// mediaMetadata describes an upload without changing its immutable image ID or URL.
type mediaMetadata struct {
	Filename, Label, Tags string
	Version               int64
}

// mediaMetadataRepository keeps names and tags separate from uploaded image bytes.
// Missing metadata describes a legacy upload, not a missing image.
type mediaMetadataRepository interface {
	ListMediaMetadata(context.Context) (map[string]mediaMetadata, error)
	GetMediaMetadata(context.Context, string) (mediaMetadata, error)
	RememberMedia(context.Context, string, string) error
	SaveMediaMetadata(context.Context, string, mediaMetadata) error
}

// ListMediaMetadata supplies search metadata for the filesystem-backed library.
// Callers ignore deleted files. Re-uploading the same bytes recovers their metadata.
func (s *sqliteRepository) ListMediaMetadata(ctx context.Context) (map[string]mediaMetadata, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, filename, label, tags, version FROM blog_media")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	metadata := make(map[string]mediaMetadata)
	for rows.Next() {
		var id string
		var value mediaMetadata
		if err := rows.Scan(&id, &value.Filename, &value.Label, &value.Tags, &value.Version); err != nil {
			return nil, err
		}
		metadata[id] = value
	}
	return metadata, rows.Err()
}

// GetMediaMetadata returns an empty value for uploads created before naming was available.
func (s *sqliteRepository) GetMediaMetadata(ctx context.Context, id string) (mediaMetadata, error) {
	var value mediaMetadata
	err := s.db.QueryRowContext(ctx, "SELECT filename, label, tags, version FROM blog_media WHERE id=?", id).Scan(&value.Filename, &value.Label, &value.Tags, &value.Version)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return value, err
}

// RememberMedia retains the first filename and never overwrites a label or tags.
func (s *sqliteRepository) RememberMedia(ctx context.Context, id, filename string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO blog_media(id, filename, label, version) VALUES (?, ?, '', 1)
ON CONFLICT(id) DO UPDATE SET filename=excluded.filename, version=blog_media.version+1
WHERE blog_media.filename='' AND excluded.filename<>''`, id, filename)
	return err
}

// SaveMediaMetadata saves the label and canonical tags together, rejecting stale edits.
// A version of zero creates metadata for a legacy upload. Filename is not editable.
func (s *sqliteRepository) SaveMediaMetadata(ctx context.Context, id string, value mediaMetadata) error {
	var result sql.Result
	var err error
	if value.Version == 0 {
		result, err = s.db.ExecContext(ctx, "INSERT INTO blog_media(id, filename, label, tags, version) VALUES (?, '', ?, ?, 1) ON CONFLICT(id) DO NOTHING", id, value.Label, value.Tags)
	} else {
		result, err = s.db.ExecContext(ctx, "UPDATE blog_media SET label=?, tags=?, version=version+1 WHERE id=? AND version=?", value.Label, value.Tags, id, value.Version)
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
