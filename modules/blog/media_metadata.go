package blog

import (
	"context"
	"database/sql"
	"errors"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// mediaMetadata describes an upload without changing its immutable ID or URL.
// Nil size means unknown. Nil upload time means completion has not been recorded.
// UploadedAt uses UTC Unix nanoseconds. Checksum is the original's SHA-256 hex.
type mediaMetadata struct {
	Filename, Label, Tags string
	UploadedBy            string
	ContentType, Checksum string
	SizeBytes, UploadedAt *int64
	Version               int64
}

// mediaMetadataRepository keeps metadata separate from uploaded bytes.
// Missing metadata describes a legacy upload, not a missing file.
type mediaMetadataRepository interface {
	ListMediaMetadata(context.Context, ...string) (map[string]mediaMetadata, error)
	GetMediaMetadata(context.Context, string) (mediaMetadata, error)
	RememberMedia(ctx context.Context, id string, value mediaMetadata) error
	RecordMediaUpload(ctx context.Context, id string, completedAt time.Time) error
	SaveMediaMetadata(context.Context, string, mediaMetadata) error
}

// ListMediaMetadata returns metadata for the supplied IDs, or all metadata when
// no IDs are supplied. Callers ignore deleted files. Re-uploading the same bytes
// recovers their metadata.
func (s *sqliteRepository) ListMediaMetadata(ctx context.Context, ids ...string) (map[string]mediaMetadata, error) {
	query := "SELECT id, filename, label, tags, version, uploaded_by, content_type, size_bytes, uploaded_at, checksum FROM blog_media"
	var args []any
	if len(ids) > 0 {
		query += " WHERE id IN (" + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ")"
		for _, id := range ids {
			args = append(args, id)
		}
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	metadata := make(map[string]mediaMetadata)
	for rows.Next() {
		var id string
		var value mediaMetadata
		if err := rows.Scan(&id, &value.Filename, &value.Label, &value.Tags, &value.Version, &value.UploadedBy, &value.ContentType, &value.SizeBytes, &value.UploadedAt, &value.Checksum); err != nil {
			return nil, err
		}
		metadata[id] = value
	}
	return metadata, rows.Err()
}

// GetMediaMetadata returns an empty value for uploads created before naming was available.
func (s *sqliteRepository) GetMediaMetadata(ctx context.Context, id string) (mediaMetadata, error) {
	var value mediaMetadata
	err := s.db.QueryRowContext(ctx, "SELECT filename, label, tags, version, uploaded_by, content_type, size_bytes, uploaded_at, checksum FROM blog_media WHERE id=?", id).Scan(&value.Filename, &value.Label, &value.Tags, &value.Version, &value.UploadedBy, &value.ContentType, &value.SizeBytes, &value.UploadedAt, &value.Checksum)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return value, err
}

// RememberMedia records the first uploader. Duplicate bytes never transfer ownership.
// It fills unknown content facts but retains known values, labels, and tags.
// Upload completion is recorded separately after the file is stored.
func (s *sqliteRepository) RememberMedia(ctx context.Context, id string, value mediaMetadata) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO blog_media(id, filename, label, version, uploaded_by, content_type, size_bytes, checksum) VALUES (?, ?, '', 1, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
 filename=CASE WHEN blog_media.filename='' AND blog_media.uploaded_by=excluded.uploaded_by THEN excluded.filename ELSE blog_media.filename END,
 content_type=CASE WHEN blog_media.content_type='' THEN excluded.content_type ELSE blog_media.content_type END,
 size_bytes=coalesce(blog_media.size_bytes,excluded.size_bytes),
 checksum=CASE WHEN blog_media.checksum='' THEN excluded.checksum ELSE blog_media.checksum END,
 version=blog_media.version+1
WHERE (blog_media.filename='' AND excluded.filename<>'' AND blog_media.uploaded_by=excluded.uploaded_by)
 OR (blog_media.content_type='' AND excluded.content_type<>'')
 OR (blog_media.size_bytes IS NULL AND excluded.size_bytes IS NOT NULL)
 OR (blog_media.checksum='' AND excluded.checksum<>'')`, id, value.Filename, value.UploadedBy, value.ContentType, value.SizeBytes, value.Checksum)
	return err
}

// RecordMediaUpload records the first completed upload without changing
// ownership or the version used to edit labels and tags. Failed writes leave it NULL.
func (s *sqliteRepository) RecordMediaUpload(ctx context.Context, id string, completedAt time.Time) error {
	result, err := s.db.ExecContext(ctx, "UPDATE blog_media SET uploaded_at=coalesce(uploaded_at,?) WHERE id=?", completedAt.UnixNano(), id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		err = errNotFound
	}
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
