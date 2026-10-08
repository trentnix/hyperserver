package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHTTPCredentialFormActions(t *testing.T) {
	for _, name := range []string{"login", "register", "change"} {
		for _, htmx := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/htmx=%t", name, htmx), func(t *testing.T) {
				runHTTPScenario(t, func(h *httpHarness) {
					if name == "change" {
						h.establishSession(t, h.seedUser(t, "change@example.invalid"))
					}
					path := "/auth/" + name + "/email"
					formID := name + "-form"
					if name == "change" {
						formID = "change-password-form"
					}
					for _, method := range []string{http.MethodGet, http.MethodPost} {
						// Empty POST fields force redisplay without changing account state.
						w := h.request(method, path, url.Values{}, htmx)
						if w.Code >= http.StatusInternalServerError {
							t.Fatalf("%s failed: %d", method, w.Code)
						}
						form := regexp.MustCompile(`<form\s+id="` + formID + `"[^>]*>`).FindString(w.Body.String())
						for _, attribute := range []string{`method="post"`, `action="` + path + `"`, `hx-post="` + path + `"`} {
							if !regexp.MustCompile(`\s` + regexp.QuoteMeta(attribute)).MatchString(form) {
								t.Errorf("%s form missing %s: %s", method, attribute, form)
							}
						}
					}
				})
			})
		}
	}
}

// Exercise the browser's native submission with real rendered forms, but no HTMX.
// Submission handlers inspect transport only. HTTP tests cover account operations.
func TestBrowserNativeCredentialForms(t *testing.T) {
	if os.Getenv("HS_TEST_FIREFOX") == "" {
		t.Skip("set HS_TEST_FIREFOX to a Firefox executable to run browser tests")
	}
	runHTTPScenarioWithTimeout(t, 75*time.Second, func(h *httpHarness) {
		const secret = "Native&A+B1!"
		forms := []struct {
			path   string
			fields url.Values
			body   string
		}{
			{path: "/auth/login/email", fields: url.Values{"email": {"native@example.invalid"}, "password": {secret}}},
			{path: "/auth/register/email", fields: url.Values{"email": {"native@example.invalid"}, "password": {secret}, "passwordMatch": {secret}}},
			{path: "/auth/change/email", fields: url.Values{"oldPassword": {secret}, "newPassword": {secret}, "newPasswordMatch": {secret}}},
			{path: "/auth/logout", fields: url.Values{}},
		}
		for i := range forms {
			if forms[i].path == "/auth/change/email" {
				h.establishSession(t, h.seedUser(t, "native@example.invalid"))
			}
			path := forms[i].path
			if path == "/auth/logout" {
				path = "/" // Logout is a POST form in the full-page navigation.
			}
			w := h.request(http.MethodGet, path, nil, path != "/")
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "<form") {
				t.Fatalf("render %s: status %d", forms[i].path, w.Code)
			}
			forms[i].body = w.Body.String()
			if forms[i].path == "/auth/logout" {
				forms[i].body = regexp.MustCompile(`(?s)<form class="logout-form".*?</form>`).FindString(forms[i].body)
				if forms[i].body == "" {
					t.Fatal("navigation logout form is missing")
				}
			}
		}

		ready := make(chan struct{})
		var readyOnce sync.Once
		result := make(chan string, 1)
		report := func(message string) {
			select {
			case result <- message:
			default:
			}
		}
		page := func(w http.ResponseWriter, body string) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			io.WriteString(w, `<!doctype html><html><head><script src="/test/diagnostics/errors.js"></script></head><body>`+body+`<script src="/test/submit.js"></script></body></html>`)
		}
		mux := http.NewServeMux()
		mux.HandleFunc("POST /test/ready", func(w http.ResponseWriter, r *http.Request) {
			readyOnce.Do(func() { close(ready) })
			w.WriteHeader(http.StatusNoContent)
		})
		mux.HandleFunc("GET /test/submit.js", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/javascript")
			io.WriteString(w, `document.addEventListener("DOMContentLoaded", async () => {
  const ready = await fetch("/test/ready", {method: "POST"});
  if (!ready.ok) throw new Error("Could not report browser readiness");
  if (typeof htmx !== "undefined") throw new Error("HTMX must be unavailable");
  const form = document.querySelector("form");
  for (const input of form.querySelectorAll("input")) {
    if (input.type === "email") input.value = "native@example.invalid";
    if (input.type === "password") input.value = "Native&A+B1!";
  }
  form.requestSubmit();
});`)
		})
		mux.HandleFunc("/test/start", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.RawQuery != "" {
				report("form submitted to the document URL instead of its action")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			page(w, forms[0].body)
		})
		for i, form := range forms {
			mux.HandleFunc(form.path, func(w http.ResponseWriter, r *http.Request) {
				r.Body = http.MaxBytesReader(w, r.Body, 4096)
				if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.Header.Get("HX-Request") != "" {
					report(form.path + ": expected native POST without query parameters")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if err := r.ParseForm(); err != nil || !reflect.DeepEqual(r.PostForm, form.fields) {
					report(form.path + ": incorrect POST fields")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if i+1 < len(forms) {
					page(w, forms[i+1].body)
					return
				}
				report("PASS")
				w.WriteHeader(http.StatusNoContent)
			})
		}
		browserLogs := &capturedLogs{}
		server := httptest.NewServer(browserDiagnosticHandler(mux, result, browserLogs))
		defer server.Close()
		runBrowser(t, server.URL+"/test/start", ready, result, browserLogs.String)
	})
}
