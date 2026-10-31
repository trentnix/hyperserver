package blog

import (
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/trentnix/hyperserver/pkg/components/htmx"
	"github.com/trentnix/hyperserver/pkg/util"
)

// finishContentDelete updates an existing management list without navigating.
// The current URL only selects response behavior. It is never a redirect target.
func finishContentDelete(w http.ResponseWriter, r *http.Request, kind, listPath string) {
	request := htmx.GetRequest(r)
	current, err := url.Parse(r.Header.Get("HX-Current-URL"))
	if request.Enabled && !request.HistoryRestore && err == nil && current.Path == listPath {
		payload, _ := json.Marshal(map[string]map[string]string{
			"blog:content-deleted": {"kind": kind, "id": r.PathValue("id")},
		})
		htmx.Response{NoContent: true, Trigger: string(payload)}.Apply(w)
		return
	}
	util.RedirectToURL(w, r, listPath)
}
