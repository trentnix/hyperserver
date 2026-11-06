package blog

import (
	"errors"
	"net/http"

	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/util"
)

// publishPost publishes the viewed draft without accepting replacement content.
func (m *module) publishPost(w http.ResponseWriter, r *http.Request) {
	if parseError := form.Parse(r); parseError != nil {
		m.app.ContentManager.HandleError(w, r, parseError.Message, nil, parseError.StatusCode)
		return
	}
	version, valid := editorVersion(r)
	if !valid {
		m.app.ContentManager.HandleError(w, r, "Reload the draft before publishing. Its version is missing or invalid.", nil, http.StatusBadRequest)
		return
	}
	p, err := m.posts.GetByID(r.Context(), r.PathValue("id"), true)
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	if p.Version != version || !p.Draft {
		m.app.ContentManager.HandleError(w, r, "This post changed since you opened it. Reload and review it before publishing.", nil, http.StatusConflict)
		return
	}
	p.Draft = false
	saved, err := m.writePost(r.Context(), p)
	var invalid contentError
	switch {
	case errors.Is(err, errEditConflict):
		m.app.ContentManager.HandleError(w, r, "This post changed since you opened it. Reload and review it before publishing.", nil, http.StatusConflict)
	case errors.Is(err, errAccessDenied):
		m.app.ContentManager.HandleError(w, r, err.Error(), nil, http.StatusForbidden)
	case errors.As(err, &invalid), errors.Is(err, errMissingMedia):
		m.app.ContentManager.HandleError(w, r, err.Error(), nil, http.StatusUnprocessableEntity)
	case err != nil:
		m.storageError(w, r, err)
	default:
		util.RedirectToURL(w, r, "/post/s/"+saved.Slug)
	}
}
