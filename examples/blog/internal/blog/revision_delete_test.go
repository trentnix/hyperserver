package blog

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// revisionDeleteFixture creates four snapshots and returns newest-first metadata.
func revisionDeleteFixture(t *testing.T, h *harness, kind string) (string, []Revision) {
	t.Helper()
	store := h.blog.posts.(*sqliteRepository)
	var id string
	var version int64
	for _, title := range []string{"First", "Second", "Third", "Current"} {
		var err error
		id, version, err = saveRevisionContent(t.Context(), store, kind, id, version, Revision{Title: title, Markdown: title + " body"})
		if err != nil {
			t.Fatal(err)
		}
	}
	list, err := store.ListRevisions(t.Context(), kind, id)
	if err != nil || len(list) != 4 {
		t.Fatal(list, err)
	}
	return id, list
}

// revisionOwner reads all current fields so history deletion cannot change settings or edit tokens.
func revisionOwner(t *testing.T, h *harness, kind, id string) any {
	t.Helper()
	if kind == "post" {
		p, err := h.blog.posts.GetByID(t.Context(), id, true)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	p, err := h.blog.pageStore.GetPageByID(t.Context(), id, true)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestDeleteRevisionStorage covers atomic protection, owner isolation, cancellation,
// repeated deletion, and availability after the last historical snapshot is removed.
func TestDeleteRevisionStorage(t *testing.T) {
	for _, kind := range []string{"post", "page"} {
		t.Run(kind, func(t *testing.T) {
			h := newHarness(t)
			store := h.blog.posts.(*sqliteRepository)
			id, list := revisionDeleteFixture(t, h, kind)
			before := revisionOwner(t, h, kind, id)
			for _, tc := range []struct {
				kind, owner string
				number      int64
				want        error
			}{
				{kind, id, list[0].ID, errCurrentRevision},
				{kind, "other-owner", list[1].ID, errNotFound},
				{"unknown", id, list[1].ID, errNotFound},
				{kind, id, -1, errNotFound},
			} {
				if err := store.DeleteRevision(t.Context(), tc.kind, tc.owner, tc.number); !errors.Is(err, tc.want) {
					t.Fatalf("delete %+v: %v", tc, err)
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if err := store.DeleteRevision(ctx, kind, id, list[1].ID); !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation", err)
			}

			var wg sync.WaitGroup
			results := make(chan error, 2)
			for range 2 {
				wg.Go(func() { results <- store.DeleteRevision(t.Context(), kind, id, list[1].ID) })
			}
			wg.Wait()
			close(results)
			success := 0
			for err := range results {
				if err == nil {
					success++
				} else if !errors.Is(err, errNotFound) {
					t.Fatal("concurrent delete", err)
				}
			}
			if success != 1 {
				t.Fatal("expected exactly one successful deletion", success)
			}
			for _, revision := range list[2:] {
				if err := store.DeleteRevision(t.Context(), kind, id, revision.ID); err != nil {
					t.Fatal(err)
				}
			}
			remaining, err := store.ListRevisions(t.Context(), kind, id)
			if err != nil || !reflect.DeepEqual(remaining, list[:1]) {
				t.Fatal("current snapshot changed", remaining, err)
			}
			if available, err := store.HasHistory(t.Context(), kind, id); err != nil || available {
				t.Fatal("obsolete history availability", available, err)
			}
			if owners, err := store.HistoryOwners(t.Context(), kind); err != nil || owners[id] {
				t.Fatal("obsolete management history", owners, err)
			}
			if !reflect.DeepEqual(before, revisionOwner(t, h, kind, id)) {
				t.Fatal("deletion changed current content or settings")
			}
		})
	}
}

// TestDeleteRevisionDuringSave checks the latest-snapshot guard while a new version is saved.
func TestDeleteRevisionDuringSave(t *testing.T) {
	for _, kind := range []string{"post", "page"} {
		t.Run(kind, func(t *testing.T) {
			h := newHarness(t)
			store := h.blog.posts.(*sqliteRepository)
			id, list := revisionDeleteFixture(t, h, kind)
			start := make(chan struct{})
			saved, deleted := make(chan error, 1), make(chan error, 1)
			go func() {
				<-start
				_, _, err := saveRevisionContent(t.Context(), store, kind, id, 4, Revision{Title: "Newest", Markdown: "New writing"})
				saved <- err
			}()
			go func() {
				<-start
				deleted <- store.DeleteRevision(t.Context(), kind, id, list[0].ID)
			}()
			close(start)
			saveErr, deleteErr := <-saved, <-deleted
			if saveErr != nil || (deleteErr != nil && !errors.Is(deleteErr, errCurrentRevision)) {
				t.Fatal("concurrent save and delete", saveErr, deleteErr)
			}
			remaining, err := store.ListRevisions(t.Context(), kind, id)
			if err != nil || len(remaining) < 4 || remaining[0].Title != "Newest" {
				t.Fatal("lost current snapshot", remaining, err)
			}
			if err := store.DeleteRevision(t.Context(), kind, id, remaining[0].ID); !errors.Is(err, errCurrentRevision) {
				t.Fatal("new current snapshot is not protected", err)
			}
		})
	}
}

// failedRevisionDelete exercises failure reporting without changing stored history.
type failedRevisionDelete struct{ revisionRepository }

func (failedRevisionDelete) DeleteRevision(context.Context, string, string, int64) error {
	return errors.New("private storage failure")
}

// TestDeleteRevisionHTTP verifies authorization, confirmation, current-version protection,
// safe navigation, and identical behavior for ordinary and HTMX requests.
func TestDeleteRevisionHTTP(t *testing.T) {
	for _, kind := range []string{"post", "page"} {
		for _, hx := range []bool{false, true} {
			t.Run(kind+"/htmx="+strconv.FormatBool(hx), func(t *testing.T) {
				h := newHarness(t)
				id, list := revisionDeleteFixture(t, h, kind)
				// Exercise published content through ordinary requests and drafts through HTMX.
				if hx {
					if kind == "post" {
						if _, err := h.blog.posts.SetPublished(t.Context(), id, false); err != nil {
							t.Fatal(err)
						}
					} else {
						p := revisionOwner(t, h, kind, id).(Page)
						p.Published, p.ParentID, p.DisplayOrder, p.ChildrenDisplay = false, h.projectsID, new(7), "blocks"
						if _, err := h.blog.pageStore.SavePage(t.Context(), p); err != nil {
							t.Fatal(err)
						}
					}
				}
				before := revisionOwner(t, h, kind, id)
				base := "/history/" + kind + "/" + id
				deleteURL := func(number int64) string { return base + "/delete/" + strconv.FormatInt(number, 10) }
				for _, signedIn := range []bool{false, true} {
					if signedIn {
						h.register(t)
						h.login(t)
					}
					for _, method := range []string{"GET", "POST"} {
						requireStatus(t, h.request(method, deleteURL(list[1].ID), nil, hx), http.StatusForbidden)
					}
				}
				h.verify(t)
				for _, method := range []string{"GET", "POST"} {
					requireStatus(t, h.request(method, deleteURL(list[0].ID), nil, hx), http.StatusConflict)
					for _, path := range []string{base + "/delete/invalid", deleteURL(0), deleteURL(-1), "/history/unknown/" + id + "/delete/1", "/history/" + kind + "/other-owner/delete/" + strconv.FormatInt(list[1].ID, 10)} {
						requireStatus(t, h.request(method, path, nil, hx), http.StatusNotFound)
					}
				}
				requireStatus(t, h.request("PUT", deleteURL(list[1].ID), nil, hx), http.StatusNotFound)
				current := h.request("GET", base, nil, hx)
				requireStatus(t, current, http.StatusOK)
				if strings.Contains(current.Body.String(), "Delete revision") {
					t.Fatal("current version offers deletion")
				}
				confirm := h.request("GET", deleteURL(list[1].ID), nil, hx)
				requireStatus(t, confirm, http.StatusOK)
				if !strings.Contains(confirm.Body.String(), "This cannot be undone.") || !strings.Contains(confirm.Body.String(), "Cancel") || strings.Contains(confirm.Body.String(), "<dialog") != hx {
					t.Fatal("missing confirmation or incorrect presentation", confirm.Body.String())
				}
				afterGET, err := h.blog.history.ListRevisions(t.Context(), kind, id)
				if err != nil || !reflect.DeepEqual(afterGET, list) {
					t.Fatal("GET or rejected requests changed history", afterGET, err)
				}
				history := h.blog.history
				h.blog.history = failedRevisionDelete{history}
				failure := h.request("POST", deleteURL(list[1].ID), nil, hx)
				requireStatus(t, failure, http.StatusInternalServerError)
				if failure.Header().Get("Location") != "" || failure.Header().Get("HX-Redirect") != "" || failure.Header().Get("HX-Trigger") != "" || strings.Contains(failure.Body.String(), "private storage failure") {
					t.Fatal("storage failure leaked or redirected as success")
				}
				h.blog.history = history

				contentURL := "/post/s/history"
				if kind == "page" {
					contentURL = "/page/history"
				}
				for _, tc := range []struct {
					number      int64
					destination string
				}{
					{list[1].ID, base + "?revision=" + strconv.FormatInt(list[2].ID, 10)},
					{list[3].ID, base + "?revision=" + strconv.FormatInt(list[2].ID, 10)},
					{list[2].ID, contentURL},
				} {
					response := h.request("POST", deleteURL(tc.number), nil, hx)
					if hx && tc.destination != contentURL {
						requireStatus(t, response, http.StatusNoContent)
						var events map[string]map[string]string
						if err := json.Unmarshal([]byte(response.Header().Get("HX-Trigger")), &events); err != nil {
							t.Fatal(err)
						}
						detail := events["blog:revision-deleted"]
						if detail["revision"] != strconv.FormatInt(tc.number, 10) || detail["next"] != tc.destination || response.Header().Get("HX-Redirect") != "" {
							t.Fatal("incorrect in-place deletion response", response.Header())
						}
						requireStatus(t, h.request("POST", deleteURL(tc.number), nil, hx), http.StatusNotFound)
						continue
					}
					status, header := http.StatusSeeOther, "Location"
					if hx {
						status, header = http.StatusOK, "HX-Redirect"
					}
					requireStatus(t, response, status)
					if response.Header().Get(header) != tc.destination || response.Header().Get("Cache-Control") != "no-store" {
						t.Fatal("incorrect redirect", response.Header())
					}
					requireStatus(t, h.request("POST", deleteURL(tc.number), nil, hx), http.StatusNotFound)
				}
				if !reflect.DeepEqual(before, revisionOwner(t, h, kind, id)) {
					t.Fatal("deletion changed current content or settings")
				}
			})
		}
	}
}
