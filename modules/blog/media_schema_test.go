package blog

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The same metadata and reference tables can hold images, documents, and audio.
// These are storage tests, not new upload routes or page component support.
func TestMediaSchema(t *testing.T) {
	for _, source := range []string{"startup", "migration"} {
		t.Run(source, func(t *testing.T) {
			db, filename := schemaTestDB(t)
			if source == "migration" {
				run := blogMigrationCommand(t, filename, "migrations")
				if output, err := run("up"); err != nil {
					t.Fatal(output, err)
				}
			}
			if err := prepareBlogSchema(t.Context(), db); err != nil {
				t.Fatal(err)
			}
			seed, err := os.ReadFile("testdata/migration-content.sql")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(t.Context(), string(seed)); err != nil {
				t.Fatal(err)
			}
			s := &sqliteRepository{db: db}
			for _, contentType := range []string{"image/png", "application/pdf", "audio/mpeg", "video/mp4", "text/plain"} {
				size, uploadedAt := int64(0), int64(1700000000000000000)
				value := mediaMetadata{Filename: "original", UploadedBy: "owner", ContentType: contentType, SizeBytes: &size, Checksum: strings.Repeat("a", 64)}
				if err := s.RememberMedia(t.Context(), contentType, value); err != nil {
					t.Fatal(err)
				}
				if err := s.RecordMediaUpload(t.Context(), contentType, time.Unix(0, uploadedAt)); err != nil {
					t.Fatal(err)
				}
				value.UploadedAt = &uploadedAt
				value.Version = 1
				got, err := s.GetMediaMetadata(t.Context(), contentType)
				if err != nil || !reflect.DeepEqual(got, value) {
					t.Fatalf("%s metadata: %+v, %v", contentType, got, err)
				}
				listed, err := s.ListMediaMetadata(t.Context(), contentType)
				if err != nil || !reflect.DeepEqual(listed[contentType], value) {
					t.Fatal("metadata list lost upload facts", listed, err)
				}
				// Both reference tables enforce revision ownership and uniqueness.
				for _, table := range []string{"blog_post_media", "blog_page_media"} {
					query := "INSERT INTO " + table + "(version_id,url) VALUES(4,?)"
					if _, err := db.ExecContext(t.Context(), query, "/files/"+contentType); err != nil {
						t.Fatal(err)
					}
					if _, err := db.ExecContext(t.Context(), query, "/files/"+contentType); err == nil {
						t.Fatal("duplicate reference accepted")
					}
					if _, err := db.ExecContext(t.Context(), "INSERT INTO "+table+"(version_id,url) VALUES(-1,?)", contentType); err == nil {
						t.Fatal("missing revision accepted")
					}
				}
			}
			for _, change := range []string{"size_bytes=-1", "uploaded_at=0", "checksum='invalid'"} {
				if _, err := db.ExecContext(t.Context(), "UPDATE blog_media SET "+change); err == nil {
					t.Fatal("invalid upload metadata accepted", change)
				}
			}
			if _, err := db.ExecContext(t.Context(), `DELETE FROM blog_posts WHERE id=1; DELETE FROM blog_pages WHERE id=2`); err != nil {
				t.Fatal(err)
			}
			for _, check := range []string{"SELECT count(*) FROM blog_post_media WHERE version_id=1", "SELECT count(*) FROM blog_page_media WHERE version_id=2"} {
				var count int
				if err := db.QueryRowContext(t.Context(), check).Scan(&count); err != nil || count != 0 {
					t.Fatal("revision deletion retained references", count, err)
				}
			}
		})
	}
}

func TestMediaMetadataFillsUnknownFacts(t *testing.T) {
	db, _ := schemaTestDB(t)
	if err := prepareBlogSchema(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	s := &sqliteRepository{db: db}
	if err := s.RecordMediaUpload(t.Context(), "missing", time.Now()); !errors.Is(err, errNotFound) {
		t.Fatal("completion accepted missing metadata", err)
	}
	if err := s.RememberMedia(t.Context(), "document", mediaMetadata{Filename: "first.pdf", UploadedBy: "owner"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMediaMetadata(t.Context(), "document", mediaMetadata{Label: "Library label", Tags: "kept", Version: 1}); err != nil {
		t.Fatal(err)
	}
	size, uploadedAt := int64(10), int64(1700000000000000000)
	value := mediaMetadata{Filename: "second.pdf", UploadedBy: "another", ContentType: "application/pdf", SizeBytes: &size, UploadedAt: &uploadedAt, Checksum: strings.Repeat("b", 64)}
	if err := s.RememberMedia(t.Context(), "document", value); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordMediaUpload(t.Context(), "document", time.Unix(0, uploadedAt)); err != nil {
		t.Fatal(err)
	}
	want := value
	want.Filename, want.UploadedBy, want.Label, want.Tags, want.Version = "first.pdf", "owner", "Library label", "kept", 3
	got, err := s.GetMediaMetadata(t.Context(), "document")
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("backfill changed attribution or library metadata", got, err)
	}
	// Duplicate uploads and label edits must not replace known immutable facts.
	size, uploadedAt = 20, uploadedAt+1
	value.ContentType, value.Checksum = "text/plain", strings.Repeat("c", 64)
	if err := s.RememberMedia(t.Context(), "document", value); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordMediaUpload(t.Context(), "document", time.Unix(0, uploadedAt)); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMediaMetadata(t.Context(), "document", mediaMetadata{Label: "Edited", Version: 3}); err != nil {
		t.Fatal(err)
	}
	want = got
	want.Label, want.Tags, want.Version = "Edited", "", 4
	got, err = s.GetMediaMetadata(t.Context(), "document")
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("duplicate upload or label edit replaced upload facts", got, err)
	}
}
