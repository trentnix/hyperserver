package blog

import (
	"net/http"

	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/services/logger"
)

// previewSocialPost renders unsaved input with the published component renderer.
// Providers load in the browser. This endpoint does not fetch or save anything.
func (m *module) previewSocialPost(w http.ResponseWriter, r *http.Request) {
	if err := form.ParseWithOptions(r, form.ParseOptions{MaxFieldBytes: 8 * 1024}); err != nil {
		http.Error(w, err.Message, err.StatusCode)
		return
	}
	source, err := joinPostComponents("", componentInput{"socialPost": r.PostForm.Get("socialPost"), "componentOrder": `["social-post"]`})
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	rendered, err := markdown(source)
	if err != nil {
		http.Error(w, "Unable to preview the social media post.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := w.Write([]byte(rendered)); err != nil {
		logger.LogRequestError(r, err)
	}
}
