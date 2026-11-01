package blog

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestEditorRecoveryScopes confines browser copies to the signed-in administrator.
func TestEditorRecoveryScopes(t *testing.T) {
	h := newHarness(t)
	response := h.request("GET", "/", nil, false)
	if strings.Contains(response.Body.String(), "editor-drafts.js") || strings.Contains(response.Body.String(), "data-draft-owner") {
		t.Fatal("public pages must not initialize draft recovery")
	}
	h.register(t)
	h.verify(t)
	h.login(t)
	account, err := h.app.Accounts.GetByEmail(t.Context(), "trentnix@gmail.com")
	if err != nil {
		t.Fatal(err)
	}
	for _, hx := range []bool{false, true} {
		for _, path := range []string{"/post/create", "/pages/create", "/pages/update/" + h.projectsID} {
			response := h.request("GET", path, nil, hx)
			requireStatus(t, response, http.StatusOK)
			if !strings.Contains(response.Body.String(), `data-draft-owner="`+account.ID+`"`) || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("editor must supply its administrator scope without caching")
			}
		}
	}
}

// TestEditorSaveAcknowledgment clears recovery only after a successful content write.
func TestEditorSaveAcknowledgment(t *testing.T) {
	for _, path := range []string{"/post", "/pages"} {
		t.Run(path, func(t *testing.T) {
			h := newHarness(t)
			h.register(t)
			h.verify(t)
			h.login(t)
			values := url.Values{"title": {"Saved content"}, "slug": {"saved"}, "content": {"Writing"}, "childrenDisplay": {"hidden"}}
			response := h.request("POST", path, values, true)
			requireStatus(t, response, http.StatusOK)
			if response.Header().Get("HX-Trigger") != "blog:content-saved" {
				t.Fatal("successful write must acknowledge saving before redirecting")
			}
			for _, fields := range []url.Values{values, {"title": {""}}} {
				response := h.request("POST", path, fields, true)
				if response.Code < 400 || response.Header().Get("HX-Trigger") != "" {
					t.Fatal("conflict or validation failure must not discard recovery")
				}
			}
			h.blog.posts = failingRepository{h.blog.posts}
			h.blog.pageStore = unavailablePages{}
			values.Set("slug", "storage-failure")
			response = h.request("POST", path, values, true)
			requireStatus(t, response, http.StatusInternalServerError)
			if response.Header().Get("HX-Trigger") != "" {
				t.Fatal("storage failure must not discard recovery")
			}
		})
	}
}
