package models

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type failingContactFile struct {
	*os.File
	writeErr   error
	afterClose func() error
}

func (file *failingContactFile) Write(data []byte) (int, error) {
	if file.writeErr != nil {
		// Leave a partial file so the test exercises cleanup after a failed write.
		n, err := file.File.Write(data[:len(data)/2])
		if err != nil {
			return n, err
		}
		return n, file.writeErr
	}
	return file.File.Write(data)
}

func (file *failingContactFile) Close() error {
	if err := file.File.Close(); err != nil {
		return err
	}
	if file.afterClose != nil {
		return file.afterClose()
	}
	return nil
}

func TestFileContactRepositoryIOFailures(t *testing.T) {
	for _, stage := range []string{"write", "close", "publication"} {
		t.Run(stage, func(t *testing.T) {
			directory := t.TempDir()
			repository, err := NewFileContactRepository(context.Background(), directory)
			if err != nil {
				t.Fatal(err)
			}
			want := errors.New("injected " + stage + " failure")
			var opened *os.File
			var existingPath string
			repository.createTemp = func(directory, pattern string) (contactFile, error) {
				file, err := os.CreateTemp(directory, pattern)
				if err != nil {
					return nil, err
				}
				opened = file
				t.Cleanup(func() { file.Close() })
				wrapped := &failingContactFile{File: file}
				switch stage {
				case "write":
					wrapped.writeErr = want
				case "close":
					wrapped.afterClose = func() error { return want }
				case "publication":
					want = os.ErrExist
					wrapped.afterClose = func() error {
						data, err := os.ReadFile(file.Name())
						if err != nil {
							t.Fatal(err)
						}
						var created ContactSubmission
						if err := json.Unmarshal(data, &created); err != nil {
							t.Fatal(err)
						}
						// Force a real hard-link failure without relying on permissions or timing.
						existingPath = filepath.Join(directory, created.Id+".json")
						if err := os.WriteFile(existingPath, []byte("existing data"), 0600); err != nil {
							t.Fatal(err)
						}
						return nil
					}
				}
				return wrapped, nil
			}
			submission := ContactSubmission{Id: "original", Name: "Person", Email: "person@example.invalid", Message: "Hello", CreatedAt: time.Unix(1, 0)}
			before := submission
			if err := repository.Create(context.Background(), &submission); !errors.Is(err, want) {
				t.Fatalf("Create error = %v, want %v", err, want)
			}
			if submission != before {
				t.Fatal("failed write changed its submission")
			}
			if opened == nil {
				t.Fatal("test did not open a temporary file")
			}
			if _, err := opened.WriteString("unexpected write"); !errors.Is(err, os.ErrClosed) {
				t.Fatalf("temporary file remains open: %v", err)
			}

			wantEntries := 0
			if existingPath != "" {
				wantEntries = 1
				data, err := os.ReadFile(existingPath)
				if err != nil || string(data) != "existing data" {
					t.Fatalf("publication changed existing data: %q, %v", data, err)
				}
			}
			entries, err := os.ReadDir(directory)
			if err != nil || len(entries) != wantEntries {
				t.Fatalf("directory entries = %v, %v, want only preexisting files", entries, err)
			}
		})
	}
}

func TestFileContactRepositoryConcurrentWrites(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "contacts")
	repository, err := NewFileContactRepository(context.Background(), directory)
	if err != nil {
		t.Fatal(err)
	}
	const count = 8
	submissions := make([]ContactSubmission, count)
	errors := make([]error, count)
	var work sync.WaitGroup
	for i := range submissions {
		work.Go(func() {
			submissions[i] = ContactSubmission{Name: "Person", Email: "person@example.invalid", Message: "Hello <world>"}
			errors[i] = repository.Create(context.Background(), &submissions[i])
		})
	}
	work.Wait()
	for i, submission := range submissions {
		if errors[i] != nil {
			t.Fatal(errors[i])
		}
		data, err := os.ReadFile(filepath.Join(directory, submission.Id+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var stored ContactSubmission
		if err := json.Unmarshal(data, &stored); err != nil {
			t.Fatal(err)
		}
		if stored.Id == "" || stored.Id != submission.Id || stored.Name != submission.Name || stored.Email != submission.Email || stored.Message != submission.Message || !stored.CreatedAt.Equal(submission.CreatedAt) {
			t.Fatalf("incorrect stored submission: %+v", stored)
		}
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != count {
		t.Fatalf("directory entries = %d, %v, want %d complete files and no temporary files", len(entries), err, count)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			t.Fatalf("unexpected file %q", entry.Name())
		}
	}
	if _, err := NewFileContactRepository(context.Background(), directory); err != nil {
		t.Fatal("reopening existing storage:", err)
	}
}

func TestFileContactRepositoryFailures(t *testing.T) {
	for _, name := range []string{"nil submission", "invalid email", "canceled request", "storage unavailable"} {
		t.Run(name, func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "contacts")
			repository, err := NewFileContactRepository(context.Background(), directory)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			submission := &ContactSubmission{Email: "person@example.invalid"}
			switch name {
			case "nil submission":
				submission = nil
			case "invalid email":
				submission.Email = "invalid"
			case "canceled request":
				cancel()
			case "storage unavailable":
				if err := os.Remove(directory); err != nil {
					t.Fatal(err)
				}
			}
			var before ContactSubmission
			if submission != nil {
				before = *submission
			}
			if err := repository.Create(ctx, submission); err == nil {
				t.Fatal("failed write reported success")
			}
			if submission != nil && *submission != before {
				t.Fatal("failed write changed its submission")
			}
			if name != "storage unavailable" {
				entries, err := os.ReadDir(directory)
				if err != nil || len(entries) != 0 {
					t.Fatalf("failed write left files: %v, %v", entries, err)
				}
			}
		})
	}
}

func TestFileContactRepositorySetupErrors(t *testing.T) {
	for _, name := range []string{"empty directory", "file instead of directory", "canceled setup"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			path := filepath.Join(t.TempDir(), "contacts")
			switch name {
			case "empty directory":
				path = ""
			case "file instead of directory":
				if err := os.WriteFile(path, []byte("existing data"), 0600); err != nil {
					t.Fatal(err)
				}
			case "canceled setup":
				cancel()
			}
			if repository, err := NewFileContactRepository(ctx, path); repository != nil || err == nil {
				t.Fatalf("invalid setup returned %v, %v", repository, err)
			}
			if name == "file instead of directory" {
				data, err := os.ReadFile(path)
				if err != nil || string(data) != "existing data" {
					t.Fatalf("setup changed existing data: %q, %v", data, err)
				}
			}
		})
	}
}
