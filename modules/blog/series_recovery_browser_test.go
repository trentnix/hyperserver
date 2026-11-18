package blog

import (
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestBrowserSeriesRecovery(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check series recovery")
	}
	for _, mode := range []string{"pending", "recover_none", "stale_untouched", "stale", "removed", "series_only"} {
		t.Run(mode, func(t *testing.T) {
			var sourceID, collectionID string
			testBrowser(t, browser, "390", browserScenario{
				scriptFile: "testdata/series-recovery.js", startPath: "/post/manage", authenticated: true,
				setup: func(t *testing.T) *harness {
					h := registeredBrowserHarness(t)
					h.login(t)
					first, second := seriesPost(t, h, "Review first"), seriesPost(t, h, "Review second")
					value, err := h.blog.writeSeries(adminWritingContext(t.Context()), Series{Title: "Review shared series"}, []string{first.ID, second.ID})
					if err != nil {
						t.Fatal(err)
					}
					collectionID, sourceID = value.ID, first.ID
					if mode == "pending" {
						sourceID = seriesPost(t, h, "Review unassigned").ID
					}
					return h
				},
				prepareScript: func(t *testing.T, _ *harness, source []byte) []byte {
					return []byte(strings.NewReplacer("TEST_MODE", mode, "SOURCE_ID", sourceID).Replace(string(source)))
				},
				routes: func(h *harness) map[string]http.HandlerFunc {
					return map[string]http.HandlerFunc{
						"/test/change-series": func(w http.ResponseWriter, r *http.Request) {
							value, err := h.blog.series.GetSeries(r.Context(), collectionID)
							if err != nil {
								t.Error(err)
								http.Error(w, "lookup failed", http.StatusInternalServerError)
								return
							}
							value.Title = "Changed in another editor"
							ids := seriesSelection(value).Posts
							if mode == "removed" {
								ids = []string{}
							}
							if _, err := h.blog.writeSeries(adminWritingContext(r.Context()), *value, ids); err != nil {
								t.Error(err)
								http.Error(w, "save failed", http.StatusInternalServerError)
								return
							}
							w.WriteHeader(http.StatusNoContent)
						},
					}
				},
			})
		})
	}
}
