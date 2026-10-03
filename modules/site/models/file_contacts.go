package models

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// FileContactRepository stores one JSON file per contact submission. It owns no
// open files between calls and needs no Close method. It provides no transactions
// with other repositories. The directory must be private to the application.
type FileContactRepository struct {
	directory  string
	createTemp func(string, string) (contactFile, error)
}

// contactFile keeps file creation replaceable for testing I/O failures.
type contactFile interface {
	io.WriteCloser
	Name() string
}

// NewFileContactRepository creates the directory and checks file and hard-link
// support. Filesystem calls cannot be interrupted by ctx. Cancellation is checked
// before and after setup. Existing directory permissions are left unchanged.
func NewFileContactRepository(ctx context.Context, directory string) (*FileContactRepository, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if directory == "" {
		return nil, fmt.Errorf("a contact directory must be specified")
	}
	directory, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, fmt.Errorf("prepare contact directory: %w", err)
	}
	probe, err := os.CreateTemp(directory, ".contact-probe-*")
	if err != nil {
		return nil, fmt.Errorf("check contact directory: %w", err)
	}
	closeErr := probe.Close()
	linkPath := probe.Name() + ".link"
	linkErr := os.Link(probe.Name(), linkPath)
	if linkErr == nil {
		linkErr = os.Remove(linkPath)
	}
	removeErr := os.Remove(probe.Name())
	if closeErr != nil {
		return nil, closeErr
	}
	if removeErr != nil {
		return nil, removeErr
	}
	if linkErr != nil {
		return nil, fmt.Errorf("check contact file publication: %w", linkErr)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &FileContactRepository{
		directory: directory,
		createTemp: func(directory, pattern string) (contactFile, error) {
			return os.CreateTemp(directory, pattern)
		},
	}, nil
}

// Create publishes a complete JSON file without overwriting an existing file.
// Publication uses a hard link, so the filesystem must support hard links. The
// caller's submission is unchanged on failure. Cancellation after publication
// does not undo the write. This does not guarantee durability across power loss.
func (repository *FileContactRepository) Create(ctx context.Context, submission *ContactSubmission) error {
	if submission == nil {
		return fmt.Errorf("a contact submission must be specified")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !IsValidEmail(submission.Email) {
		return NewErrInvalidEmail(fmt.Errorf("failed to create a new contact_submission record"))
	}
	created := *submission
	created.Id = uuid.New().String()
	created.CreatedAt = time.Now()

	file, err := repository.createTemp(repository.directory, ".contact-*")
	if err != nil {
		return fmt.Errorf("create contact file: %w", err)
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := json.NewEncoder(file).Encode(&created); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Link(file.Name(), filepath.Join(repository.directory, created.Id+".json")); err != nil {
		return fmt.Errorf("publish contact file: %w", err)
	}
	*submission = created
	return nil
}
