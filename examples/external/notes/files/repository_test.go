package files

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"example.com/greeting/notes"
)

func testNote() notes.Note {
	return notes.Note{ID: "00000000-0000-4000-8000-000000000001", Text: "saved", CreatedAt: time.Now().UTC()}
}

func TestInvalidStoredRecords(t *testing.T) {
	note := testNote()
	valid, err := json.Marshal(record{Version: 1, Note: note})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, data string }{
		{"malformed", "{"},
		{"trailing", string(valid) + "{}"},
		{"unknown version", strings.Replace(string(valid), `"version":1`, `"version":2`, 1)},
		{"wrong ID", strings.Replace(string(valid), note.ID, "00000000-0000-4000-8000-000000000002", 1)},
		{"invalid note", strings.Replace(string(valid), `"text":"saved"`, `"text":""`, 1)},
		{"oversized", strings.Repeat("a", maxRecordBytes+1)},
	} {
		for _, layout := range []string{"flat", "directory"} {
			t.Run(layout+"/"+tc.name, func(t *testing.T) {
				directory := t.TempDir()
				r, err := Open(t.Context(), directory)
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(directory, note.ID+".json")
				if layout == "directory" {
					directory := filepath.Join(directory, note.ID+".note")
					if err := os.Mkdir(directory, 0700); err != nil {
						t.Fatal(err)
					}
					path = filepath.Join(directory, "note.json")
				}
				if err := os.WriteFile(path, []byte(tc.data), 0600); err != nil {
					t.Fatal(err)
				}
				if got, err := r.List(t.Context(), 100); err == nil || got != nil {
					t.Fatalf("corrupt file returned success: %v, %v", got, err)
				}
			})
		}
	}
}

func TestFilePublicationAndOutage(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "notes")
	r, err := Open(t.Context(), directory)
	if err != nil {
		t.Fatal(err)
	}
	note := testNote()
	if err := r.Create(t.Context(), note); err != nil {
		t.Fatal(err)
	}
	if err := r.Create(t.Context(), note); !errors.Is(err, notes.ErrExists) {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 || entries[0].Name() != note.ID+".note" || !entries[0].IsDir() {
		t.Fatalf("publication left temporary files: %v %v", entries, err)
	}
	contents, err := os.ReadDir(filepath.Join(directory, entries[0].Name()))
	if err != nil || len(contents) != 1 || contents[0].Name() != "note.json" {
		t.Fatalf("published record = %v, %v", contents, err)
	}
	interrupted := filepath.Join(directory, ".note-interrupted")
	if err := os.Mkdir(interrupted, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(interrupted, "note.json"), []byte("partial record"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := r.List(t.Context(), 100); err != nil || len(got) != 1 {
		t.Fatalf("interrupted write was visible: %v %v", got, err)
	}
	if err := os.Rename(directory, directory+"-offline"); err != nil {
		t.Fatal(err)
	}
	if err := r.Create(t.Context(), testNote()); err == nil {
		t.Fatal("unavailable storage accepted a write")
	}
	if got, err := r.List(t.Context(), 100); err == nil || got != nil {
		t.Fatal("unavailable storage returned success")
	}
}

func TestExistingFlatRecord(t *testing.T) {
	directory := t.TempDir()
	repository, err := Open(t.Context(), directory)
	if err != nil {
		t.Fatal(err)
	}
	note := testNote()
	data, err := json.Marshal(record{Version: 1, Note: note})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, note.ID+".json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := repository.Create(t.Context(), note); !errors.Is(err, notes.ErrExists) {
		t.Fatalf("existing flat record = %v", err)
	}
	if got, err := repository.List(t.Context(), 100); err != nil || !reflect.DeepEqual(got, []notes.Note{note}) {
		t.Fatalf("existing notes = %v, %v", got, err)
	}
}

func TestFailedPublicationCleansTemporaryDirectory(t *testing.T) {
	directory := t.TempDir()
	repository, err := Open(t.Context(), directory)
	if err != nil {
		t.Fatal(err)
	}
	note := testNote()
	path := filepath.Join(directory, note.ID+".note")
	if err := os.WriteFile(path, []byte("existing file"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := repository.Create(t.Context(), note); !errors.Is(err, notes.ErrExists) {
		t.Fatalf("conflicting destination = %v", err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "existing file" {
		t.Fatalf("conflicting destination changed: %s, %v", data, err)
	}
	if entries, err := os.ReadDir(directory); err != nil || len(entries) != 1 {
		t.Fatalf("failed publication left temporary files: %v, %v", entries, err)
	}
}

func TestIncompletePublishedDirectory(t *testing.T) {
	directory := t.TempDir()
	repository, err := Open(t.Context(), directory)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, testNote().ID+".note")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if got, err := repository.List(t.Context(), 100); err == nil || got != nil {
		t.Fatalf("incomplete record returned success: %v, %v", got, err)
	}
}

func TestFileStartupAndNonRegularRecord(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	path := filepath.Join(t.TempDir(), "not-created")
	if _, err := Open(ctx, path); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("canceled startup created storage")
	}
	if err := os.WriteFile(path, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(t.Context(), path); err == nil {
		t.Fatal("accepted a regular file as directory")
	}
	r, err := Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(r.directory, testNote().ID+".json"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := r.List(t.Context(), 100); err == nil {
		t.Fatal("accepted a directory as a record")
	}
}
