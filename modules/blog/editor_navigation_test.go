package blog

import (
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Cancel returns to the viewed content, including after an ordinary failed save.
// Editors opened directly or from management keep their list destination.
func TestEditorReturnNavigation(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	editLink := regexp.MustCompile(`<a class="action-edit[^"]*" href="([^"]+)"`)
	cancelLink := regexp.MustCompile(`<a class="button-secondary" href="([^"]+)"[^>]*>Cancel</a>`)
	for _, kind := range []string{"post", "page"} {
		id, revisions := revisionDeleteFixture(t, h, kind)
		prefix, manager, source := "/post", "/post/manage", "/post/id/"+id+"?view=reader"
		if kind == "page" {
			p, err := h.blog.pageStore.GetPageByID(t.Context(), id, true)
			if err != nil {
				t.Fatal(err)
			}
			prefix, manager, source = "/pages", "/pages", p.URL()+"?view=reader"
		}
		checkCancel := func(body, want string) {
			t.Helper()
			match := cancelLink.FindStringSubmatch(body)
			if len(match) != 2 || html.UnescapeString(match[1]) != want {
				t.Fatalf("Cancel = %v, want %q", match, want)
			}
		}
		for _, hx := range []bool{false, true} {
			view := h.request(http.MethodGet, source, nil, hx)
			requireStatus(t, view, http.StatusOK)
			match := editLink.FindStringSubmatch(view.Body.String())
			if len(match) != 2 {
				t.Fatalf("%s has no Edit link", source)
			}
			editURL := html.UnescapeString(match[1])
			response := h.request(http.MethodGet, editURL, nil, hx)
			requireStatus(t, response, http.StatusOK)
			checkCancel(response.Body.String(), source)
			if !strings.Contains(response.Body.String(), `name="returnTo" value="`+html.EscapeString(source)+`"`) {
				t.Fatal("editor did not retain its destination for ordinary submissions")
			}
			response = h.request(http.MethodGet, prefix+"/update/"+id, nil, hx)
			requireStatus(t, response, http.StatusOK)
			checkCancel(response.Body.String(), manager)
			for _, destination := range []string{"https://outside.example/", "//outside.example/", "/%2foutside.example/", "/\\outside.example/"} {
				response = h.request(http.MethodGet, prefix+"/update/"+id+"?returnTo="+url.QueryEscape(destination), nil, hx)
				requireStatus(t, response, http.StatusOK)
				checkCancel(response.Body.String(), manager)
			}
			revisionID := strconv.FormatInt(revisions[0].ID, 10)
			response = h.request(http.MethodGet, prefix+"/update/"+id+"?revision="+revisionID+"&returnTo="+url.QueryEscape(source), nil, hx)
			requireStatus(t, response, http.StatusOK)
			checkCancel(response.Body.String(), "/history/"+kind+"/"+id+"?revision="+revisionID)
		}
		fields := url.Values{"title": {""}, "content": {"Body"}, "returnTo": {source}, "childrenDisplay": {"hidden"}}
		response := h.request(http.MethodPost, prefix+"/"+id, fields, false)
		requireStatus(t, response, http.StatusUnprocessableEntity)
		checkCancel(response.Body.String(), source)
		fields.Set("returnTo", "https://outside.example/")
		response = h.request(http.MethodPost, prefix+"/"+id, fields, false)
		requireStatus(t, response, http.StatusUnprocessableEntity)
		checkCancel(response.Body.String(), manager)
	}
}
