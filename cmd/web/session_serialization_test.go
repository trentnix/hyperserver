package main

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/config"
)

func TestHTTPSessionCounterJSON(t *testing.T) {
	for _, provider := range []string{"cookieStore", "sqliteStore"} {
		t.Run(provider, func(t *testing.T) {
			runHTTPScenario(t, func(h *httpHarness) {
				for count := 1; count <= 3; count++ {
					w := h.request(http.MethodPost, "/session-example", nil, false)
					if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), fmt.Sprintf("# of My Visits: %d", count)) {
						t.Fatalf("visit %d: %d %s", count, w.Code, w.Body.String())
					}
				}
			}, func(cfg *config.Config) {
				cfg.HTTP.Session.Types["counterSession"] = provider
			})
		})
	}
}
