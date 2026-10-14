package storage

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/trentnix/hyperserver/modules/site/models"
)

func TestContactFileBaselineReadable(t *testing.T) {
	data, err := os.ReadFile("testdata/contact-unversioned.json")
	if err != nil {
		t.Fatal(err)
	}
	var got models.ContactSubmission
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	wantTime := time.Date(2026, time.October, 14, 12, 34, 56, 123456789, time.FixedZone("", -5*60*60))
	if got.Id != "92464e33-fd8b-47bd-b25e-c15a931893e4" || got.Name != "Zoë 李" || got.Email != "Visitor@example.invalid" || got.Message != "Hello <world> & friends\nSecond line" || !got.CreatedAt.Equal(wantTime) {
		t.Fatalf("baseline fields changed: %+v", got)
	}
	_, offset := got.CreatedAt.Zone()
	if offset != -5*60*60 || got.CreatedAt.Nanosecond() != 123456789 {
		t.Fatalf("baseline timestamp lost offset or precision: %s", got.CreatedAt)
	}
}

func TestContactFileWrittenFormat(t *testing.T) {
	for _, tc := range []struct {
		name  string
		empty bool
	}{
		{"populated", false},
		{"empty optional fields", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			directory := t.TempDir()
			repository, err := NewFileContactRepository(t.Context(), directory)
			if err != nil {
				t.Fatal(err)
			}
			submission := models.ContactSubmission{Id: "caller-id", Name: "Zoë 李", Email: "Visitor@example.invalid", Message: "Hello <world> & friends\nSecond line", CreatedAt: time.Unix(1, 0)}
			if tc.empty {
				submission.Name, submission.Message = "", ""
			}
			started := time.Now()
			if err := repository.Create(t.Context(), &submission); err != nil {
				t.Fatal(err)
			}
			id, err := uuid.Parse(submission.Id)
			if err != nil || id.Version() != 4 || submission.CreatedAt.Before(started) || submission.CreatedAt.After(time.Now()) {
				t.Fatalf("generated identity or timestamp is invalid: %+v, %v", submission, err)
			}
			data, err := os.ReadFile(filepath.Join(directory, submission.Id+".json"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasSuffix(data, []byte("\n")) {
				t.Fatal("published JSON must end with a newline")
			}

			// Independent field names catch model changes that would silently
			// change both encoding and decoding in a struct round-trip test.
			var fields map[string]string
			if err := json.Unmarshal(data, &fields); err != nil {
				t.Fatal(err)
			}
			want := map[string]string{
				"Id":        submission.Id,
				"Name":      submission.Name,
				"Email":     submission.Email,
				"Message":   submission.Message,
				"CreatedAt": submission.CreatedAt.Format(time.RFC3339Nano),
			}
			if !reflect.DeepEqual(fields, want) {
				t.Fatalf("stored format changed: got %v, want %v", fields, want)
			}
		})
	}
}

func TestContactFileSetupAndWritesPreserveExistingFiles(t *testing.T) {
	baseline, err := os.ReadFile("testdata/contact-unversioned.json")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	existing := map[string][]byte{
		"92464e33-fd8b-47bd-b25e-c15a931893e4.json": baseline,
		"unsupported.json":                          []byte(`{"format":99,"data":{}}`),
		"malformed.json":                            []byte(`{"Id":`),
		".contact-orphan":                           []byte("partial unpublished data"),
	}
	for name, data := range existing {
		if err := os.WriteFile(filepath.Join(directory, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	assertUnchanged := func() {
		t.Helper()
		for name, want := range existing {
			got, err := os.ReadFile(filepath.Join(directory, name))
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("existing file %s changed: %q, %v", name, got, err)
			}
		}
	}

	for i := range 2 {
		repository, err := NewFileContactRepository(t.Context(), directory)
		if err != nil {
			t.Fatal(err)
		}
		assertUnchanged()
		if err := repository.Create(t.Context(), &models.ContactSubmission{Email: "new@example.invalid"}); err != nil {
			t.Fatal(err)
		}
		assertUnchanged()
		entries, err := os.ReadDir(directory)
		if err != nil || len(entries) != len(existing)+i+1 {
			t.Fatalf("unexpected files after reopening and creating: %v, %v", entries, err)
		}
	}
}
