package blog

import (
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/trentnix/hyperserver/pkg/components/htmx"
	"github.com/trentnix/hyperserver/pkg/components/messages"
	"github.com/trentnix/hyperserver/pkg/util"
)

// finishContentDelete updates an existing management list without navigating.
// The current URL only selects response behavior. It is never a redirect target.
func (m *module) finishContentDelete(w http.ResponseWriter, r *http.Request, kind, listPath string) {
	request := htmx.GetRequest(r)
	current, err := url.Parse(r.Header.Get("HX-Current-URL"))
	if request.Enabled && !request.HistoryRestore && err == nil && current.Path == listPath {
		query := strings.TrimSpace(current.Query().Get("q"))
		if !utf8.ValidString(query) || utf8.RuneCountInString(query) > 240 {
			util.RedirectToURL(w, r, listPath)
			return
		}
		var data any
		view, partial, message := "posts", "blog.postResults", "Post deleted."
		if kind == "post" {
			data, err = m.postManagement(r.Context(), query)
		} else {
			view, partial, message = "pages", "blog.pageResults", "Page deleted."
			data, err = m.pageManagement(r.Context(), query)
		}
		if err != nil {
			m.app.ContentManager.HandleError(w, r, "Content deleted, but the list could not be refreshed. Reload this page.", err, http.StatusInternalServerError)
			return
		}
		page := m.views[view].Content(r, data)
		page.PartialName = partial
		page.LayoutData = m.populateNavigation(r, page.LayoutData.(navigation))
		page.OutOfBand = []string{"blog.pageLinkUpdates", "blog.closeDialog"}
		if kind == "post" {
			page.OutOfBand[0] = "blog.recentUpdates"
		}
		page.Notifications = []messages.Notification{messages.NewNotification(message, messages.NotificationTypeSuccess)}
		page.HTMX.Response = &htmx.Response{Retarget: "#management-results", Reswap: "innerHTML show:none"}
		if err := page.Render(w, r); err != nil {
			page.HandleRenderError(w, r, "Content deleted, but the list could not be refreshed. Reload this page.", err)
		}
		return
	}
	util.RedirectToURL(w, r, listPath)
}
