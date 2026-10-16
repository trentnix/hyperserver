// Package files stores example notes as JSON records in individual directories.
package files

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"example.com/greeting/notes"
)

const maxRecordBytes = 16 * 1024

// Repository uses a private directory on a filesystem supporting atomic directory renames.
// It keeps no files open between calls and needs no Close method. Filesystem calls
// are not interruptible. Context cancellation is checked between calls.
type Repository struct{ directory string }

type record struct {
	Version int        `json:"version"`
	Note    notes.Note `json:"note"`
}

// Open creates the directory and checks that it is writable.
// Existing permissions are unchanged. The application must restrict access on
// filesystems that do not enforce Unix permissions. Do not allow untrusted writers.
func Open(ctx context.Context, directory string) (*Repository, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if directory == "" {
		return nil, fmt.Errorf("notes directory is required")
	}
	directory, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}

	probe, err := os.CreateTemp(directory, ".note-probe-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(probe.Name())
	if err := probe.Close(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &Repository{directory: directory}, nil
}

// Create publishes a complete version-1 record without overwriting an existing ID.
// A failed pre-publication write remains invisible to List. Publication does not
// promise durability across power loss. Temporary directories left by a crash are ignored.
func (r *Repository) Create(ctx context.Context, note notes.Note) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := notes.Validate(note); err != nil {
		return err
	}

	// Preserve notes written by the earlier flat-file layout. Stop old application
	// processes before using the new writer against the same directory.
	if _, err := os.Lstat(filepath.Join(r.directory, note.ID+".json")); err == nil {
		return notes.ErrExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	directory, err := os.MkdirTemp(r.directory, ".note-*")
	if err != nil {
		return err
	}
	defer os.Remove(directory)
	path := filepath.Join(directory, "note.json")
	defer os.Remove(path)
	data, err := json.Marshal(record{Version: 1, Note: note})
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	// Renaming a complete, nonempty directory publishes the record at once.
	// A directory containing an existing record cannot be replaced by Rename.
	destination := filepath.Join(r.directory, note.ID+".note")
	if err := os.Rename(directory, destination); err != nil {
		if _, statErr := os.Lstat(destination); statErr == nil {
			return notes.ErrExists
		}
		return err
	}
	return nil
}

// List scans records in bounded batches, retaining only the newest limit notes.
// Work grows with directory size. Corrupt records return errors, never an empty
// success. Concurrent appends may appear on this or the next listing.
func (r *Repository) List(ctx context.Context, limit int) ([]notes.Note, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > notes.MaxListSize {
		return nil, fmt.Errorf("invalid note list limit")
	}

	directory, err := os.Open(r.directory)
	if err != nil {
		return nil, err
	}
	defer directory.Close()

	var result []notes.Note
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entries, readErr := directory.ReadDir(64)
		if readErr != nil && readErr != io.EOF {
			return nil, readErr
		}
		for _, entry := range entries {
			isDirectoryRecord := strings.HasSuffix(entry.Name(), ".note")
			if !isDirectoryRecord && !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			if (isDirectoryRecord && !entry.IsDir()) || (!isDirectoryRecord && !entry.Type().IsRegular()) {
				return nil, fmt.Errorf("invalid note record type")
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			note, err := r.read(entry.Name())
			if err != nil {
				return nil, err
			}
			result = append(result, note)
		}

		slices.SortFunc(result, func(a, b notes.Note) int {
			if order := b.CreatedAt.Compare(a.CreatedAt); order != 0 {
				return order
			}
			return strings.Compare(a.ID, b.ID)
		})
		if len(result) > limit {
			clear(result[limit:])
			result = result[:limit]
		}
		if readErr == io.EOF {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *Repository) read(name string) (notes.Note, error) {
	path := filepath.Join(r.directory, name)
	id := strings.TrimSuffix(name, ".json")
	if strings.HasSuffix(name, ".note") {
		id = strings.TrimSuffix(name, ".note")
		path = filepath.Join(path, "note.json")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return notes.Note{}, err
	}
	if !info.Mode().IsRegular() {
		return notes.Note{}, fmt.Errorf("note record must be a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return notes.Note{}, err
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maxRecordBytes+1))
	if err := errors.Join(readErr, file.Close()); err != nil {
		return notes.Note{}, err
	}
	if len(data) > maxRecordBytes {
		return notes.Note{}, fmt.Errorf("note record is too large")
	}

	var value record
	if err := json.Unmarshal(data, &value); err != nil {
		return notes.Note{}, err
	}
	if value.Version != 1 || value.Note.ID != id {
		return notes.Note{}, fmt.Errorf("invalid note record version or filename")
	}
	if err := notes.Validate(value.Note); err != nil {
		return notes.Note{}, err
	}
	return value.Note, nil
}
