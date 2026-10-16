package main

import (
	"context"
	"fmt"

	"example.com/greeting/notes"
	notesfiles "example.com/greeting/notes/files"
	notessqlite "example.com/greeting/notes/sqlite"
)

// Storage selection and resource ownership belong to application setup.
func openNotes(ctx context.Context, provider, path string) (notes.Repository, func() error, error) {
	switch provider {
	case "files":
		if path == "" {
			path = "notes-data"
		}
		repository, err := notesfiles.Open(ctx, path)
		if err != nil {
			return nil, nil, err
		}
		return repository, nil, nil
	case "sqlite":
		if path == "" {
			path = "notes.db"
		}
		repository, err := notessqlite.Open(ctx, path)
		if err != nil {
			return nil, nil, err
		}
		return repository, repository.Close, nil
	default:
		return nil, nil, fmt.Errorf("unknown notes store %q", provider)
	}
}
