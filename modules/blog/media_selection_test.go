package blog

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mattn/go-sqlite3"
)

func TestMediaUsageSelection(t *testing.T) {
	db, _ := schemaTestDB(t)
	if err := prepareBlogSchema(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	store := &sqliteRepository{db: db, now: time.Now}
	ids := []string{strings.Repeat("a", 64) + ".png", strings.Repeat("b", 64) + ".jpeg", strings.Repeat("c", 64) + ".gif"}
	for _, kind := range []string{"post", "page"} {
		body := fmt.Sprintf("![One](/images/%s?size=1#image) [Old host](https://old.example/images/%s) ![Two](//images/images/%s) ![Three](../../images/%s)", ids[0], ids[0], ids[1], ids[2])
		id, version, err := saveRevisionContent(t.Context(), store, kind, "", 0, Revision{Title: "Older title", Markdown: body, PreviewImage: ids[0]})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := saveRevisionContent(t.Context(), store, kind, id, version, Revision{Title: "Current title", Markdown: "![Current](//old.example:8080/images/" + ids[1] + ")"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, profile := range []Profile{
		{Name: "Old profile", ImageURL: "https://old.example/images/%61" + ids[0][1:] + "?size=1#portrait", FaviconURL: "/images/" + ids[0], BackgroundURL: "//old.example/images/" + ids[2]},
		{Name: "Current profile", ImageURL: "//old.example/images/" + ids[1], Version: 1},
	} {
		if err := store.SaveProfile(t.Context(), profile); err != nil {
			t.Fatal(err)
		}
	}
	all, err := store.ReferencedImages(t.Context())
	if err != nil || len(all) != len(ids) {
		t.Fatalf("all usage = %+v, %v", all, err)
	}
	if all[ids[0]].Current || len(all[ids[0]].History) != 3 || !all[ids[1]].Current || len(all[ids[2]].History) != 3 {
		t.Fatalf("fixture lost current or historical references: %+v", all)
	}
	missing := strings.Repeat("d", 64) + ".png"
	for _, selected := range [][]string{ids[:1], ids[1:], {ids[0], ids[0], missing}, {missing}} {
		got, err := store.ReferencedImages(t.Context(), selected...)
		if err != nil {
			t.Fatal(err)
		}
		want := make(map[string]mediaUsage)
		for _, id := range selected {
			if usage, ok := all[id]; ok {
				want[id] = usage
			}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("selected %v: got %+v, want %+v", selected, got, want)
		}
	}
	batch := make([]string, maxMediaDeleteBatch)
	for i := range batch {
		batch[i] = fmt.Sprintf("%064x.png", i)
	}
	batch[0], batch[len(batch)-1] = ids[0], ids[1]
	if got, err := store.ReferencedImages(t.Context(), batch...); err != nil || len(got) != 2 || !reflect.DeepEqual(got[ids[0]], all[ids[0]]) || !reflect.DeepEqual(got[ids[1]], all[ids[1]]) {
		t.Fatalf("maximum deletion batch lost usage: count=%d, error=%v", len(got), err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.ReferencedImages(ctx, ids...); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled usage lookup = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReferencedImages(t.Context(), ids[0]); err == nil {
		t.Fatal("closed storage returned successful usage")
	}
}

func TestMediaMetadataSelection(t *testing.T) {
	db, _ := schemaTestDB(t)
	if err := prepareBlogSchema(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	store := &sqliteRepository{db: db}
	for _, id := range []string{"first.png", "second.png", "deleted.png"} {
		if err := store.RememberMedia(t.Context(), id, id, "owner"); err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.ListMediaMetadata(t.Context(), "first.png", "first.png", "legacy.png", "' OR 1=1 --")
	if err != nil || len(got) != 1 || got["first.png"].Filename != "first.png" {
		t.Fatalf("selected metadata = %+v, %v", got, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.ListMediaMetadata(ctx, "first.png"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled metadata lookup = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListMediaMetadata(t.Context(), "first.png"); err == nil {
		t.Fatal("closed storage returned successful metadata")
	}
}

type observedMediaMetadata struct {
	mediaMetadataRepository
	requests [][]string
}

func (s *observedMediaMetadata) ListMediaMetadata(ctx context.Context, ids ...string) (map[string]mediaMetadata, error) {
	s.requests = append(s.requests, slices.Clone(ids))
	return s.mediaMetadataRepository.ListMediaMetadata(ctx, ids...)
}

type observedMediaReferences struct {
	mediaReferenceRepository
	requests [][]string
}

func (s *observedMediaReferences) ReferencedImages(ctx context.Context, ids ...string) (map[string]mediaUsage, error) {
	s.requests = append(s.requests, slices.Clone(ids))
	return s.mediaReferenceRepository.ReferencedImages(ctx, ids...)
}

func TestMediaLibraryMetadataScope(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	var files []mediaFile
	for i := range mediaPageSize + 2 {
		id := fmt.Sprintf("%064x.png", i)
		files = append(files, mediaFile{ID: id, Modified: time.Unix(100, 0)})
		if err := h.blog.mediaMetadata.RememberMedia(t.Context(), id, "Needle.png", ""); err != nil {
			t.Fatal(err)
		}
	}
	h.blog.media = listedMedia{files: files}
	metadata := &observedMediaMetadata{mediaMetadataRepository: h.blog.mediaMetadata}
	references := &observedMediaReferences{mediaReferenceRepository: h.blog.mediaReferences}
	h.blog.mediaMetadata, h.blog.mediaReferences = metadata, references
	for _, hx := range []bool{false, true} {
		for _, tc := range []struct {
			query       string
			first, last int
			full        bool
			usageReads  int
		}{
			{query: "pick=1", last: mediaPageSize},
			{query: "pick=1&page=2", first: mediaPageSize, last: len(files)},
			{query: "usage=unused", last: mediaPageSize, usageReads: 1},
			{query: "q=needle", full: true, usageReads: 1},
		} {
			metadata.requests, references.requests = nil, nil
			w := h.request("GET", "/media?"+tc.query, nil, hx)
			requireStatus(t, w, http.StatusOK)
			if len(metadata.requests) != 1 || len(references.requests) != tc.usageReads {
				t.Fatalf("%s: unexpected metadata or usage reads: %v, %v", tc.query, metadata.requests, references.requests)
			}
			var want []string
			if !tc.full {
				for _, file := range files[tc.first:tc.last] {
					want = append(want, file.ID)
				}
			}
			if !slices.Equal(metadata.requests[0], want) {
				t.Fatalf("%s: metadata IDs = %v, want %v", tc.query, metadata.requests[0], want)
			}
			if tc.usageReads > 0 && len(references.requests[0]) != 0 {
				t.Fatal("text and usage filtering must include all references")
			}
		}
	}
	h.blog.media = listedMedia{}
	metadata.requests, references.requests = nil, nil
	requireStatus(t, h.request("GET", "/media", nil, false), http.StatusOK)
	if len(metadata.requests)+len(references.requests) != 0 {
		t.Fatal("an empty library performed collection-wide reads")
	}
}

func TestSingleMediaUsageScope(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	file := mediaUpload(t, h.blog, "scoped usage")
	references := &observedMediaReferences{mediaReferenceRepository: h.blog.mediaReferences}
	h.blog.mediaReferences = references
	for _, hx := range []bool{false, true} {
		for _, path := range []string{"/media/" + file.ID, "/media/delete/" + file.ID} {
			references.requests = nil
			requireStatus(t, h.request("GET", path, nil, hx), http.StatusOK)
			if !reflect.DeepEqual(references.requests, [][]string{{file.ID}}) {
				t.Fatalf("%s: usage requests = %v", path, references.requests)
			}
		}
	}
	references.requests = nil
	values := url.Values{"version": {fmt.Sprint(file.Version())}}
	requireStatus(t, h.request("POST", "/media/delete/"+file.ID, values, false), http.StatusSeeOther)
	if !reflect.DeepEqual(references.requests, [][]string{{file.ID}}) {
		t.Fatalf("delete usage requests = %v", references.requests)
	}
}

func TestMediaBatchUsageScope(t *testing.T) {
	h := newHarness(t)
	unused := mediaUpload(t, h.blog, "unused batch image")
	protected := mediaUpload(t, h.blog, "protected batch image")
	if err := h.blog.persistProfile(t.Context(), Profile{Name: "Profile", ImageURL: "/images/" + protected.ID, Version: h.blog.profile.Current().Version}); err != nil {
		t.Fatal(err)
	}
	references := &observedMediaReferences{mediaReferenceRepository: h.blog.mediaReferences}
	h.blog.mediaReferences = references
	result, err := h.blog.removeUnusedMediaBatch(t.Context(), []mediaFile{unused, protected})
	if err != nil || result.Deleted != 1 || result.Skipped != 1 || result.Remaining != 0 {
		t.Fatalf("batch result = %+v, %v", result, err)
	}
	if !reflect.DeepEqual(references.requests, [][]string{{unused.ID, protected.ID}}) {
		t.Fatalf("batch usage requests = %v", references.requests)
	}
	image, err := h.blog.images.Open(t.Context(), protected.ID)
	if err != nil {
		t.Fatalf("protected image was removed: %v", err)
	}
	image.Content.Close()
	if _, err := h.blog.images.Open(t.Context(), unused.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("unused image was retained: %v", err)
	}
}

// A failure after the current-content lookup must still stop both deletion
// paths. Denying a historical timestamp read leaves current queries available.
func TestMediaUsageHistoryFailuresPreventDeletion(t *testing.T) {
	for _, table := range []string{"blog_posts", "blog_pages", "blog_profile"} {
		t.Run(table, func(t *testing.T) {
			h := newHarness(t)
			file := mediaUpload(t, h.blog, "history lookup failure")
			store := h.blog.posts.(*sqliteRepository)
			for _, kind := range []string{"post", "page"} {
				id, version, err := saveRevisionContent(t.Context(), store, kind, "", 0, Revision{Title: "Earlier", PreviewImage: file.ID})
				if err != nil {
					t.Fatal(err)
				}
				if _, _, err := saveRevisionContent(t.Context(), store, kind, id, version, Revision{Title: "Current", Markdown: "No images"}); err != nil {
					t.Fatal(err)
				}
			}
			conn, err := store.db.Conn(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			err = conn.Raw(func(driver any) error {
				driver.(*sqlite3.SQLiteConn).RegisterAuthorizer(func(op int, name, column, _ string) int {
					if op == sqlite3.SQLITE_READ && name == table && column == "recorded" {
						return sqlite3.SQLITE_DENY
					}
					return sqlite3.SQLITE_OK
				})
				return nil
			})
			if closeErr := conn.Close(); err == nil {
				err = closeErr
			}
			if err != nil {
				t.Fatal(err)
			}
			if got, err := store.ReferencedImages(t.Context(), file.ID); err == nil || got != nil {
				t.Fatalf("failed history lookup returned usage: %+v, %v", got, err)
			}
			if err := h.blog.removeMedia(t.Context(), file.ID, file.Version(), true); err == nil || errors.Is(err, errMediaUsed) || errors.Is(err, errMediaHistory) {
				t.Fatalf("single deletion concealed the history lookup failure: %v", err)
			}
			if result, err := h.blog.removeUnusedMediaBatch(t.Context(), []mediaFile{file}); err == nil || result != (mediaDeleteResult{Remaining: 1}) {
				t.Fatalf("batch deletion concealed the history lookup failure: %+v, %v", result, err)
			}
			image, err := h.blog.images.Open(t.Context(), file.ID)
			if err != nil {
				t.Fatalf("failed history lookup removed the image: %v", err)
			}
			image.Content.Close()
		})
	}
}
