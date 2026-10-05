package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/middleware"
	"github.com/trentnix/hyperserver/pkg/util"
)

func TestRequestLoggingOmitsQuery(t *testing.T) {
	for _, query := range []string{
		"",
		"?",
		"?password=private-password&token=private-token",
		"?Password=private-password&TOKEN=private-token&custom=private-value",
		"?password=first-secret&password=second-secret&%74oken=encoded-secret",
		"?search=private%26value%3Dsecret&redirect=%2Faccount%3Ftoken%3Dnested-secret",
		"?password=private-password&invalid=%zz&token=private-token;extra=secret",
	} {
		t.Run(query, func(t *testing.T) {
			logs := &capturedLogs{}
			r := httptest.NewRequest(http.MethodGet, "http://example.invalid/accounts"+query, nil)
			originalURL, originalURI := *r.URL, r.RequestURI
			w := httptest.NewRecorder()
			called := false
			handler := middleware.LoggerMiddleware(&testLogger{logs: logs})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				if *r.URL != originalURL || r.RequestURI != originalURI {
					t.Error("logging changed the request URL")
				}
				id, _ := r.Context().Value(logger.RequestIDKey).(string)
				if id == "" || id != w.Header().Get("X-Request-ID") || logger.Get(r.Context()) == nil {
					t.Error("request logging lost its context or tracing ID")
				}
				logger.LogRequestError(r, errors.New("test failure"))
				w.WriteHeader(http.StatusBadRequest)
			}))
			handler.ServeHTTP(w, r)
			if !called || w.Code != http.StatusBadRequest {
				t.Fatal("logging changed handler execution or response")
			}
			// Check both incoming-request and error records. Exact fields exclude
			// query data, while the caller's original URL must remain untouched.
			for _, line := range logs.lines {
				if !strings.Contains(line, "{path /accounts}") || strings.Contains(line, "?") {
					t.Errorf("expected path-only logging: %s", line)
				}
			}
			if len(logs.lines) != 2 || *r.URL != originalURL {
				t.Fatal("missing log records or mutated original URL")
			}
		})
	}
}

func TestHTTPRequestLogsExcludeSecrets(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		h.app.Web.HandleFunc("GET /request-log-error", func(w http.ResponseWriter, r *http.Request) {
			util.HttpError(w, r, "Unable to complete request", errors.New("storage unavailable"), http.StatusInternalServerError)
		})
		for _, tc := range []struct {
			method, path string
			status       int
		}{
			{http.MethodGet, "/contact", http.StatusOK},
			{http.MethodGet, "/missing-log-route", http.StatusNotFound},
			{http.MethodPost, "/auth/login/email", http.StatusForbidden},
			{http.MethodGet, "/request-log-error", http.StatusInternalServerError},
		} {
			for _, htmx := range []bool{false, true} {
				r := httptest.NewRequest(tc.method, h.baseURL+tc.path+"?password=private-password&token=private-token&custom=private-value", nil)
				r.Header.Set("Sec-Fetch-Site", "cross-site")
				if htmx {
					r.Header.Set("HX-Request", "true")
				}
				w := httptest.NewRecorder()
				h.handler.ServeHTTP(w, r)
				if w.Code != tc.status {
					t.Fatalf("%s %s: status %d, want %d", tc.method, tc.path, w.Code, tc.status)
				}
				logs := h.logs.String()
				if strings.Contains(logs, "private-") || strings.Contains(logs, "?") {
					t.Fatal("query data reached request logs")
				}
				for _, field := range []string{
					fmt.Sprintf("{path %s}", tc.path),
					fmt.Sprintf("{method %s}", tc.method),
					fmt.Sprintf("{requestID %s}", w.Header().Get("X-Request-ID")),
				} {
					if !strings.Contains(logs, field) {
						t.Errorf("request log missing %s", field)
					}
				}
			}
		}
	})
}
