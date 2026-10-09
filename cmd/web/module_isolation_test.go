package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/session"
)

func TestApplicationsOwnModulesAndAuthProviders(t *testing.T) {
	for _, secondAuth := range []bool{true, false} {
		t.Run(map[bool]string{true: "enabled", false: "disabled"}[secondAuth], func(t *testing.T) {
			// Template paths still use the process cwd. Build both applications before
			// concurrent requests and restore the package directory for other tests.
			start, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			t.Chdir(start)
			first := newHTTPHarness(t, func(c *config.Config) {
				c.App.Name = "First application"
				c.HTTP.Session.Types["isolation-session"] = "sqliteStore"
			})
			if err := os.Chdir(start); err != nil {
				t.Fatal(err)
			}
			second := newHTTPHarness(t, func(c *config.Config) {
				c.App.Name = "Second application"
				c.Auth.Enabled = secondAuth
				c.Auth.RegistrationEnabled = false
				c.HTTP.Session.Types["isolation-session"] = "sqliteStore"
			})
			if !secondAuth {
				if second.app.AccountRepository != nil {
					t.Fatal("disabled authentication initialized accounts")
				}
				w := second.request(http.MethodGet, "/auth/login/email", nil, false)
				if w.Code != http.StatusNotFound {
					t.Fatalf("disabled auth route returned %d", w.Code)
				}
			}
			for _, h := range []*httpHarness{first, second} {
				h.app.Web.HandleFunc("GET /isolation-session", func(w http.ResponseWriter, r *http.Request) {
					s, err := session.Get(r, "isolation-session")
					if err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					if s.IsNew {
						s.Data["owner"] = h.app.Config.App.Name
					}
					if err := s.Save(w, r); err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					fmt.Fprint(w, s.Data["owner"])
				})
			}

			var requests sync.WaitGroup
			for _, h := range []*httpHarness{first, second} {
				requests.Go(func() {
					for _, path := range []string{"/", "/isolation-session"} {
						w := h.request(http.MethodGet, path, nil, false)
						if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), h.app.Config.App.Name) {
							t.Errorf("application used another module's state at %s: %d %s", path, w.Code, w.Body.String())
						}
					}
				})
			}
			requests.Wait()
			first.assertRowCount(t, "session", 1)
			second.assertRowCount(t, "session", 1)
			// Even with matching test signing keys, separate storage must not accept
			// the other application's session record.
			base, err := url.Parse(first.baseURL)
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodGet, second.baseURL+"/isolation-session", nil)
			copied := false
			for _, cookie := range first.cookies.Cookies(base) {
				if cookie.Name == "isolation-session" {
					request.AddCookie(cookie)
					copied = true
				}
			}
			if !copied {
				t.Fatal("first application did not save its session cookie")
			}
			response := httptest.NewRecorder()
			second.handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK || response.Body.String() != second.app.Config.App.Name {
				t.Fatalf("second application accepted first session state: %d %s", response.Code, response.Body.String())
			}
			form := registrationForm()
			w := first.request(http.MethodPost, "/auth/register/email", form, true)
			if w.Code != 200 {
				t.Fatalf("first registration failed: %d %s", w.Code, w.Body.String())
			}
			first.assertRowCount(t, "user", 1)
			if secondAuth {
				second.assertRowCount(t, "user", 0)
			}
			if len(first.mail.snapshot()) != 1 || len(second.mail.snapshot()) != 0 {
				t.Fatal("registration used another application's mail client")
			}
			if err := second.app.Shutdown(); err != nil {
				t.Fatal(err)
			}
			if err := second.app.Database.Ping(); err == nil {
				t.Fatal("shutdown left the second database open")
			}
			w = first.request(http.MethodGet, "/isolation-session", nil, false)
			if w.Code != http.StatusOK || w.Body.String() != first.app.Config.App.Name {
				t.Fatalf("second shutdown affected first session: %d %s", w.Code, w.Body.String())
			}
			w = first.request(http.MethodPost, "/auth/login/email", url.Values{"email": form["email"], "password": form["password"]}, true)
			if w.Code != 200 || w.Header().Get("HX-Redirect") == "" {
				t.Fatalf("second shutdown affected first login: %d %s", w.Code, w.Body.String())
			}
		})
	}
}
