package main

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/trentnix/hyperserver/config"
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
			first := newHTTPHarness(t, func(c *config.Config) { c.App.Name = "First application" })
			if err := os.Chdir(start); err != nil {
				t.Fatal(err)
			}
			second := newHTTPHarness(t, func(c *config.Config) {
				c.App.Name = "Second application"
				c.Auth.Enabled = secondAuth
				c.Auth.RegistrationEnabled = false
			})
			var requests sync.WaitGroup
			for _, h := range []*httpHarness{first, second} {
				requests.Go(func() {
					// Disabled auth's reference middleware is separate lifecycle work.
					if !h.app.Config.Auth.Enabled {
						return
					}
					w := h.request(http.MethodGet, "/", nil, false)
					if w.Code != 200 || !strings.Contains(w.Body.String(), h.app.Config.App.Name) {
						t.Errorf("application rendered another module's layout: %d %s", w.Code, w.Body.String())
					}
				})
			}
			requests.Wait()
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
			w = first.request(http.MethodPost, "/auth/login/email", url.Values{"email": form["email"], "password": form["password"]}, true)
			if w.Code != 200 || w.Header().Get("HX-Redirect") == "" {
				t.Fatalf("second shutdown affected first login: %d %s", w.Code, w.Body.String())
			}
		})
	}
}
