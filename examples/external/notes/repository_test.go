package notes_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"example.com/greeting/notes"
	notesfiles "example.com/greeting/notes/files"
	notessqlite "example.com/greeting/notes/sqlite"
)

func openRepository(t *testing.T, provider, path string) (notes.Repository, func()) {
	t.Helper()
	if provider == "files" {
		repository, err := notesfiles.Open(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
		return repository, func() {}
	}
	repository, err := notessqlite.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	close := func() {
		if err := repository.Close(); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(close)
	return repository, close
}

func fixture(n int) notes.Note {
	return notes.Note{ID: fmt.Sprintf("00000000-0000-4000-8000-%012d", n), Text: fmt.Sprintf("note %d", n), CreatedAt: time.Date(2026, 1, 1, 12, 0, 0, 123456789, time.UTC)}
}

func TestRepositoryContract(t *testing.T) {
	for _, provider := range []string{"sqlite", "files"} {
		t.Run(provider, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "notes")
			repository, close := openRepository(t, provider, path)
			if got, err := repository.List(t.Context(), 100); err != nil || len(got) != 0 {
				t.Fatalf("empty list = %v, %v", got, err)
			}
			first, second, newest := fixture(1), fixture(2), fixture(3)
			newest.CreatedAt = newest.CreatedAt.Add(time.Nanosecond)
			first.Text = "Unicode café 日本語 <script> & \"quotes\"\nsecond line"
			for _, note := range []notes.Note{second, newest, first} {
				if err := repository.Create(t.Context(), note); err != nil {
					t.Fatal(err)
				}
			}
			duplicate := first
			duplicate.Text = "must not replace original"
			if err := repository.Create(t.Context(), duplicate); !errors.Is(err, notes.ErrExists) {
				t.Fatalf("duplicate = %v", err)
			}
			want := []notes.Note{newest, first, second}
			got, err := repository.List(t.Context(), 100)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("list = %+v, %v", got, err)
			}
			got[0].Text = "caller mutation"
			if got, err := repository.List(t.Context(), 2); err != nil || !reflect.DeepEqual(got, want[:2]) {
				t.Fatalf("limited list = %+v, %v", got, err)
			}
			for _, limit := range []int{0, -1, 101} {
				if _, err := repository.List(t.Context(), limit); err == nil {
					t.Fatal("accepted invalid limit", limit)
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if err := repository.Create(ctx, fixture(4)); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if _, err := repository.List(ctx, 100); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			for _, invalid := range []notes.Note{
				{ID: "../outside", Text: "invalid ID", CreatedAt: first.CreatedAt},
				{ID: fixture(5).ID, Text: "", CreatedAt: first.CreatedAt},
				{ID: fixture(5).ID, Text: strings.Repeat("a", 1001), CreatedAt: first.CreatedAt},
				{ID: fixture(5).ID, Text: "missing timestamp"},
			} {
				if err := repository.Create(t.Context(), invalid); err == nil {
					t.Fatal("accepted invalid note")
				}
			}
			close()
			reopened, _ := openRepository(t, provider, path)
			if got, err := reopened.List(t.Context(), 100); err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("reopened = %+v, %v", got, err)
			}
		})
	}
}

func TestConcurrentRepositories(t *testing.T) {
	for _, provider := range []string{"sqlite", "files"} {
		t.Run(provider, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "notes")
			one, _ := openRepository(t, provider, path)
			two, _ := openRepository(t, provider, path)
			var workers sync.WaitGroup
			for n := range 20 {
				workers.Go(func() {
					repository := one
					if n%2 == 0 {
						repository = two
					}
					if err := repository.Create(t.Context(), fixture(n)); err != nil {
						t.Error(err)
					}
					if _, err := repository.List(t.Context(), 100); err != nil {
						t.Error(err)
					}
				})
			}
			workers.Wait()
			if got, err := one.List(t.Context(), 100); err != nil || len(got) != 20 {
				t.Fatalf("concurrent list = %d, %v", len(got), err)
			}
		})
	}
}

func TestRepositoryListAcrossBatches(t *testing.T) {
	for _, provider := range []string{"sqlite", "files"} {
		t.Run(provider, func(t *testing.T) {
			repository, _ := openRepository(t, provider, filepath.Join(t.TempDir(), "notes"))
			for n := range 130 {
				note := fixture(n)
				note.CreatedAt = note.CreatedAt.Add(time.Duration(n) * time.Nanosecond)
				if err := repository.Create(t.Context(), note); err != nil {
					t.Fatal(err)
				}
			}

			got, err := repository.List(t.Context(), notes.MaxListSize)
			if err != nil || len(got) != notes.MaxListSize {
				t.Fatalf("list length = %d, error = %v", len(got), err)
			}
			for i, note := range got {
				if note.ID != fixture(129-i).ID {
					t.Fatalf("position %d: got %s, want %s", i, note.ID, fixture(129-i).ID)
				}
			}
		})
	}
}

func TestConcurrentDuplicate(t *testing.T) {
	for _, provider := range []string{"sqlite", "files"} {
		t.Run(provider, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "notes")
			one, _ := openRepository(t, provider, path)
			two, _ := openRepository(t, provider, path)
			start := make(chan struct{})
			results := make(chan error, 2)
			for _, repository := range []notes.Repository{one, two} {
				go func() {
					<-start
					results <- repository.Create(t.Context(), fixture(1))
				}()
			}
			close(start)
			first, second := <-results, <-results
			if !((first == nil && errors.Is(second, notes.ErrExists)) || (second == nil && errors.Is(first, notes.ErrExists))) {
				t.Fatalf("concurrent creates = %v, %v", first, second)
			}
			if got, err := one.List(t.Context(), 100); err != nil || !reflect.DeepEqual(got, []notes.Note{fixture(1)}) {
				t.Fatalf("stored notes = %+v, %v", got, err)
			}
		})
	}
}
