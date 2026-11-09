package blog

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestBlogInitializationResourceCleanup(t *testing.T) {
	for _, stage := range []string{"uploads", "assets", "overlap"} {
		t.Run(stage, func(t *testing.T) {
			h := newHarness(t)
			directory := h.app.Config.App.WorkingDirectory
			settings := *h.blog.settings
			settings.Storage.Connection = "initializing.db?_journal=WAL"
			switch stage {
			case "uploads":
				settings.UploadsDirectory = filepath.Join(directory, "regular-file")
				if err := os.WriteFile(settings.UploadsDirectory, []byte("not a directory"), 0600); err != nil {
					t.Fatal(err)
				}
			case "assets":
				settings.AssetsDirectory = filepath.Join(directory, "missing-assets")
			case "overlap":
				settings.AssetsDirectory = settings.UploadsDirectory
			}
			before := blogOpenFiles(t, directory)
			m := &module{settings: &settings, access: &Access{}, accounts: h.app.Accounts}
			err := m.Init(t.Context(), h.blog.app)
			if err == nil || !strings.Contains(err.Error(), stage) {
				t.Fatalf("initialization failure at %s = %v", stage, err)
			}
			if m.close != nil || m.access.setup != nil || m.access.accounts != nil {
				t.Fatal("failed initialization published resources or access state")
			}
			if after := blogOpenFiles(t, directory); !reflect.DeepEqual(before, after) {
				t.Fatalf("failed initialization leaked handles: before %v, after %v", before, after)
			}
			if _, err := h.app.Accounts.GetByEmail(t.Context(), settings.AdminEmail); err != nil && !errors.Is(err, user.ErrUserNotFound) {
				t.Fatal("failed initialization closed borrowed accounts", err)
			}
			if _, err := h.blog.posts.List(t.Context()); err != nil {
				t.Fatal("failed initialization closed another module's storage", err)
			}

			// Retry after correcting settings, then release the successful acquisitions.
			settings = *h.blog.settings
			settings.Storage.Connection = "initializing.db?_journal=WAL"
			func() {
				if err := m.Init(t.Context(), h.blog.app); err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := m.Shutdown(context.Background()); err != nil {
						t.Error(err)
					}
				}()
				if _, err := m.posts.List(t.Context()); err != nil {
					t.Fatal("retry did not open usable storage", err)
				}
			}()
			if after := blogOpenFiles(t, directory); !reflect.DeepEqual(before, after) {
				t.Fatalf("shutdown leaked handles: before %v, after %v", before, after)
			}
		})
	}
}

// Linux exposes open database and directory handles without adding resource
// factories solely for tests. Other platforms still exercise failure and retry.
func blogOpenFiles(t *testing.T, directory string) []string {
	t.Helper()
	if runtime.GOOS != "linux" {
		return nil
	}
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, entry := range entries {
		name, err := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(name, directory+string(os.PathSeparator)) {
			files = append(files, name)
		}
	}
	sort.Strings(files)
	return files
}
