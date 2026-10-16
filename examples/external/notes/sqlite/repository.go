// Package sqlite stores example notes in SQLite.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"example.com/greeting/notes"
	"github.com/mattn/go-sqlite3"
	frameworksqlite "github.com/trentnix/hyperserver/pkg/database/sqlite"
)

// Repository owns its pool. Close it after HTTP requests have drained.
type Repository struct{ db *sql.DB }

// Open creates the initial table. Existing schemas are not migrated.
func Open(ctx context.Context, path string) (*Repository, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	db, err := frameworksqlite.Open(path)
	if err != nil {
		return nil, err
	}
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS example_notes (
		id TEXT PRIMARY KEY NOT NULL, text TEXT NOT NULL, created_at TEXT NOT NULL
	)`)
	if err == nil {
		_, err = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS example_notes_order ON example_notes (created_at DESC, id ASC)`)
	}
	if err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return &Repository{db: db}, nil
}

// Close releases the owned database pool.
func (r *Repository) Close() error { return r.db.Close() }

// Create inserts one immutable note. An existing ID is never overwritten.
func (r *Repository) Create(ctx context.Context, note notes.Note) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := notes.Validate(note); err != nil {
		return err
	}

	// Fixed-width UTC timestamps sort chronologically and preserve nanoseconds.
	_, err := r.db.ExecContext(ctx, `INSERT INTO example_notes (id, text, created_at) VALUES (?, ?, ?)`,
		note.ID, note.Text, note.CreatedAt.UTC().Format("2006-01-02T15:04:05.000000000Z"))
	var constraint sqlite3.Error
	if errors.As(err, &constraint) && constraint.ExtendedCode == sqlite3.ErrConstraintPrimaryKey {
		return notes.ErrExists
	}
	return err
}

// List returns the newest notes in the repository's stable ordering.
func (r *Repository) List(ctx context.Context, limit int) ([]notes.Note, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > notes.MaxListSize {
		return nil, fmt.Errorf("invalid note list limit")
	}

	rows, err := r.db.QueryContext(ctx, `SELECT id, text, created_at FROM example_notes ORDER BY created_at DESC, id ASC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []notes.Note
	for rows.Next() {
		var note notes.Note
		var timestamp string
		if err := rows.Scan(&note.ID, &note.Text, &timestamp); err != nil {
			return nil, err
		}
		note.CreatedAt, err = time.Parse(time.RFC3339Nano, timestamp)
		if err != nil {
			return nil, err
		}
		if err := notes.Validate(note); err != nil {
			return nil, err
		}
		result = append(result, note)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
