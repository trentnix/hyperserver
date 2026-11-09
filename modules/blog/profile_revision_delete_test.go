package blog

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// profileDeleteFixture creates four snapshots with distinct content.
func profileDeleteFixture(t *testing.T, h *harness) []ProfileRevision {
	t.Helper()
	for _, name := range []string{"First", "Second", "Current"} {
		p := h.blog.profile.Current()
		p.Name = name
		if err := h.blog.persistProfile(t.Context(), p); err != nil {
			t.Fatal(err)
		}
	}
	versions, err := h.blog.profileStore.ListProfileRevisions(t.Context())
	if err != nil || len(versions) != 4 {
		t.Fatal(versions, err)
	}
	return versions
}

// TestDeleteProfileRevisionStorage checks protection, repeated deletion, cancellation, and unchanged live content.
func TestDeleteProfileRevisionStorage(t *testing.T) {
	h := newHarness(t)
	versions := profileDeleteFixture(t, h)
	s := h.blog.profileStore
	before, err := s.GetProfile(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteProfileRevision(t.Context(), versions[0].ID); !errors.Is(err, errCurrentRevision) {
		t.Fatal("current version was not protected", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.DeleteProfileRevision(ctx, versions[1].ID); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation", err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { results <- s.DeleteProfileRevision(t.Context(), versions[1].ID) })
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, errNotFound) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatal("expected exactly one successful deletion", success)
	}
	for _, v := range versions[2:] {
		if err := s.DeleteProfileRevision(t.Context(), v.ID); err != nil {
			t.Fatal(err)
		}
	}
	remaining, err := s.ListProfileRevisions(t.Context())
	if err != nil || !reflect.DeepEqual(remaining, versions[:1]) {
		t.Fatal("latest snapshot changed", remaining, err)
	}
	if after, err := s.GetProfile(t.Context()); err != nil || after != before {
		t.Fatal("deletion changed live content or edit token", after, err)
	}
}

// failedProfileRevisionDelete exercises failure reporting without changing stored history.
type failedProfileRevisionDelete struct{ profileRepository }

func (failedProfileRevisionDelete) DeleteProfileRevision(context.Context, int64) error {
	return errors.New("private storage failure")
}

// TestDeleteProfileRevisionDuringSave keeps the latest snapshot protected regardless of operation order.
func TestDeleteProfileRevisionDuringSave(t *testing.T) {
	h := newHarness(t)
	versions := profileDeleteFixture(t, h)
	s := h.blog.profileStore
	p := h.blog.profile.Current()
	p.Name = "Newest"
	start := make(chan struct{})
	saved, deleted := make(chan error, 1), make(chan error, 1)
	go func() {
		<-start
		saved <- s.SaveProfile(t.Context(), p)
	}()
	go func() {
		<-start
		deleted <- s.DeleteProfileRevision(t.Context(), versions[0].ID)
	}()
	close(start)
	saveErr, deleteErr := <-saved, <-deleted
	if saveErr != nil || (deleteErr != nil && !errors.Is(deleteErr, errCurrentRevision)) {
		t.Fatal("concurrent save and delete", saveErr, deleteErr)
	}
	remaining, err := s.ListProfileRevisions(t.Context())
	if err != nil || len(remaining) < 4 || remaining[0].Name != "Newest" {
		t.Fatal("lost latest snapshot", remaining, err)
	}
	if err := s.DeleteProfileRevision(t.Context(), remaining[0].ID); !errors.Is(err, errCurrentRevision) {
		t.Fatal("latest snapshot is not protected", err)
	}
}

// TestDeleteProfileRevisionHTTP covers admission, confirmation, errors, and in-place history updates.
func TestDeleteProfileRevisionHTTP(t *testing.T) {
	for _, hx := range []bool{false, true} {
		t.Run(strconv.FormatBool(hx), func(t *testing.T) {
			h := newHarness(t)
			versions := profileDeleteFixture(t, h)
			path := func(id int64) string { return "/profile/history/delete/" + strconv.FormatInt(id, 10) }
			for _, signedIn := range []bool{false, true} {
				if signedIn {
					h.register(t)
					h.login(t)
				}
				for _, method := range []string{"GET", "POST"} {
					requireStatus(t, h.request(method, path(versions[1].ID), nil, hx), http.StatusForbidden)
				}
			}
			h.verify(t)
			for _, method := range []string{"GET", "POST"} {
				requireStatus(t, h.request(method, path(versions[0].ID), nil, hx), http.StatusConflict)
				for _, invalid := range []string{path(0), path(-1), path(999999), "/profile/history/delete/invalid"} {
					requireStatus(t, h.request(method, invalid, nil, hx), http.StatusNotFound)
				}
			}
			current := h.request("GET", "/profile/history", nil, hx)
			requireStatus(t, current, http.StatusOK)
			if strings.Contains(current.Body.String(), "Delete revision") {
				t.Fatal("latest version offers deletion")
			}
			confirm := h.request("GET", path(versions[1].ID), nil, hx)
			requireStatus(t, confirm, http.StatusOK)
			if !strings.Contains(confirm.Body.String(), "This cannot be undone.") || strings.Contains(activeMarkup(confirm.Body.String()), "<dialog") != hx {
				t.Fatal("incorrect confirmation", confirm.Body.String())
			}
			s := h.blog.profileStore
			if after, err := s.ListProfileRevisions(t.Context()); err != nil || !reflect.DeepEqual(after, versions) {
				t.Fatal("GET or rejected deletion changed history", after, err)
			}
			h.blog.profileStore = failedProfileRevisionDelete{s}
			failure := h.request("POST", path(versions[1].ID), nil, hx)
			requireStatus(t, failure, http.StatusInternalServerError)
			if failure.Header().Get("HX-Trigger") != "" || failure.Header().Get("Location") != "" || strings.Contains(failure.Body.String(), "private storage failure") {
				t.Fatal("failed deletion reported success or exposed internals")
			}
			h.blog.profileStore = s
			for i, v := range versions[1:] {
				response := h.request("POST", path(v.ID), nil, hx)
				next := "/profile"
				if i < 2 {
					next = "/profile/history?revision=" + strconv.FormatInt(versions[i+2].ID, 10)
				}
				if hx && i < 2 {
					requireStatus(t, response, http.StatusOK)
					if response.Header().Get("HX-Retarget") != "#revision-preview" || response.Header().Get("HX-Replace-Url") != next || !strings.Contains(response.Body.String(), `hx-swap-oob="innerHTML:#version-list"`) {
						t.Fatal("incorrect deletion fragment", response.Header())
					}
				} else {
					status, header := http.StatusSeeOther, "Location"
					if hx {
						status, header = http.StatusOK, "HX-Redirect"
					}
					requireStatus(t, response, status)
					if response.Header().Get(header) != next {
						t.Fatal("incorrect destination", response.Header())
					}
				}
				requireStatus(t, h.request("POST", path(v.ID), nil, hx), http.StatusNotFound)
			}
			profile := h.request("GET", "/profile", nil, hx)
			requireStatus(t, profile, http.StatusOK)
			if strings.Contains(profile.Body.String(), `href="/profile/history"`) {
				t.Fatal("history control remains with only the latest version")
			}
		})
	}
}

// TestDeleteProfileRevisionReleasesMedia removes only the historical reference, not the image.
func TestDeleteProfileRevisionReleasesMedia(t *testing.T) {
	h := newHarness(t)
	m := h.blog
	file := mediaUpload(t, m, "historical portrait")
	p := m.profile.Current()
	p.ImageURL = "/images/" + file.ID
	if err := m.persistProfile(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	versions, err := m.profileStore.ListProfileRevisions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	p = m.profile.Current()
	p.ImageURL = ""
	if err := m.persistProfile(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	before, err := m.mediaCandidates(t.Context())
	if err != nil || len(before.History) != 1 || len(before.Files) != 0 {
		t.Fatal("missing historical reference", before, err)
	}
	if err := m.profileStore.DeleteProfileRevision(t.Context(), versions[0].ID); err != nil {
		t.Fatal(err)
	}
	after, err := m.mediaCandidates(t.Context())
	if err != nil || len(after.History) != 0 || len(after.Files) != 1 || after.Files[0].ID != file.ID {
		t.Fatal("historical reference remains or image removed", after, err)
	}
}
