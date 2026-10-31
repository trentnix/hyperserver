package blog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// deleteRequest represents the browser's current page without accepting it as a
// redirect destination. Ordinary submissions use the same authenticated routes.
func (h *harness) deleteRequest(path, current string, hx, history bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "https://blog.example"+path, nil)
	r.Header.Set("HX-Current-URL", current)
	if hx {
		r.Header.Set("HX-Request", "true")
	}
	if history {
		r.Header.Set("HX-History-Restore-Request", "true")
	}
	for _, cookie := range h.cookies {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.app.Handler.ServeHTTP(w, r)
	return w
}

// TestContentDeleteResponses covers list events, detail-page redirects, ordinary
// forms, repeat submissions, and invalid current URLs for both content types.
func TestContentDeleteResponses(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	for _, kind := range []string{"post", "page"} {
		listPath, prefix := "/post/manage", "/post"
		if kind == "page" {
			listPath, prefix = "/pages", "/pages"
		}
		for i, tc := range []struct {
			name, current      string
			hx, history, event bool
		}{
			{"list", "https://blog.example" + listPath, true, false, true},
			{"filtered list", "https://blog.example" + listPath + "?q=needle", true, false, true},
			{"ordinary form", "https://blog.example" + listPath, false, false, false},
			{"detail", "https://blog.example/post/s/example", true, false, false},
			{"missing current URL", "", true, false, false},
			{"malformed current URL", "%", true, false, false},
			{"history request", "https://blog.example" + listPath, true, true, false},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				slug := fmt.Sprintf("delete-%s-%d", kind, i)
				var id string
				if kind == "post" {
					p, err := h.blog.posts.Save(t.Context(), Post{Slug: slug, Title: "Needle post", Draft: true})
					if err != nil {
						t.Fatal(err)
					}
					id = p.ID
				} else {
					p, err := h.blog.pageStore.SavePage(t.Context(), Page{Slug: slug, Title: "Needle page"})
					if err != nil {
						t.Fatal(err)
					}
					id = p.ID
				}
				path := prefix + "/delete/" + id
				w := h.deleteRequest(path, tc.current, tc.hx, tc.history)
				if tc.event {
					requireStatus(t, w, http.StatusNoContent)
					var payload map[string]map[string]string
					if err := json.Unmarshal([]byte(w.Header().Get("HX-Trigger")), &payload); err != nil {
						t.Fatal(err)
					}
					deleted := payload["blog:content-deleted"]
					if deleted["kind"] != kind || deleted["id"] != id || w.Header().Get("HX-Redirect") != "" || w.Header().Get("Location") != "" || w.Body.Len() != 0 {
						t.Fatal("list deletion must identify the deleted row without navigating", w)
					}
				} else {
					status, header := http.StatusSeeOther, "Location"
					if tc.hx && !tc.history {
						status, header = http.StatusOK, "HX-Redirect"
					}
					requireStatus(t, w, status)
					if w.Header().Get(header) != listPath || w.Header().Get("HX-Trigger") != "" {
						t.Fatal("deletion must redirect to the fixed management path", w)
					}
				}
				if w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("deletion response is cacheable")
				}
				repeated := h.deleteRequest(path, tc.current, tc.hx, tc.history)
				requireStatus(t, repeated, http.StatusNotFound)
				if repeated.Header().Get("HX-Trigger") != "" {
					t.Fatal("repeat deletion emitted a success event")
				}
			})
		}
	}
}

// TestContentDeleteListFailures leaves rows intact when authorization, page
// hierarchy rules, or storage prevent a deletion.
func TestContentDeleteListFailures(t *testing.T) {
	h := newHarness(t)
	post, err := h.blog.posts.Save(t.Context(), Post{Slug: "delete-protected", Title: "Protected", Draft: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/post/delete/" + post.ID, "/pages/delete/" + h.projectsID} {
		w := h.deleteRequest(path, "https://blog.example/pages", true, false)
		if w.Header().Get("HX-Trigger") != "" {
			t.Fatal("anonymous deletion emitted success")
		}
	}
	if _, err := h.blog.posts.GetByID(t.Context(), post.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := h.blog.pageStore.GetPageByID(t.Context(), h.projectsID, true); err != nil {
		t.Fatal(err)
	}
	h.register(t)
	h.verify(t)
	h.login(t)
	if _, err := h.blog.pageStore.SavePage(t.Context(), Page{Slug: "protected-child", Title: "Child", ParentID: h.projectsID}); err != nil {
		t.Fatal(err)
	}
	w := h.deleteRequest("/pages/delete/"+h.projectsID, "https://blog.example/pages", true, false)
	requireStatus(t, w, http.StatusConflict)
	if w.Header().Get("HX-Trigger") != "" || !strings.Contains(w.Body.String(), "Cannot delete page") {
		t.Fatal("blocked page deletion must keep its error dialog")
	}
	h.blog.posts = failedContentDelete{h.blog.posts}
	h.blog.pageStore = unavailablePages{}
	for _, tc := range []struct{ path, current string }{
		{"/post/delete/" + post.ID, "https://blog.example/post/manage"},
		{"/pages/delete/" + h.projectsID, "https://blog.example/pages"},
	} {
		w := h.deleteRequest(tc.path, tc.current, true, false)
		requireStatus(t, w, http.StatusInternalServerError)
		if w.Header().Get("HX-Trigger") != "" || strings.Contains(w.Body.String(), "private failure") {
			t.Fatal("failed deletion emitted success or exposed storage details")
		}
	}
}

type failedContentDelete struct{ repository }

func (failedContentDelete) Delete(context.Context, string) error {
	return errors.New("private failure")
}
