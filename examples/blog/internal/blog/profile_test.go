package blog

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestConfiguredProfile checks module-loaded settings on full pages, fragments,
// authentication pages, errors, and RSS without cross-application state.
func TestConfiguredProfile(t *testing.T) {
	for _, name := range []string{"Example Writer", "A & B <writers>"} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			cfg := *h.app.Config
			settings := *h.blog.settings
			settings.Profile = Profile{Name: name, Bio: `Notes about <software> & life.`, ImageURL: "/assets/img/project-default.svg"}
			if err := h.app.Shutdown(); err != nil {
				t.Fatal(err)
			}
			data, err := yaml.Marshal(settings)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(cfg.App.WorkingDirectory, "modules/blog/config.yaml"), data, 0600); err != nil {
				t.Fatal(err)
			}
			h = startHarness(t, cfg)
			for _, path := range []string{"/about", "/projects", loginURL, "/missing-page"} {
				w := h.request("GET", path, nil, false)
				wantStatus := http.StatusOK
				if path == "/missing-page" {
					wantStatus = http.StatusNotFound
				}
				requireStatus(t, w, wantStatus)
				body := w.Body.String()
				if !strings.Contains(body, `<title>`+html.EscapeString(name)) || !strings.Contains(body, html.EscapeString(settings.Profile.Bio)) || strings.Contains(body, "Trent Nix") || !strings.Contains(body, `src="/assets/img/project-default.svg"`) {
					t.Fatalf("configured identity missing or unescaped on %s: %s", path, body)
				}
			}
			r := httptest.NewRequest("GET", "/about", nil)
			r.Header.Set("HX-Request", "true")
			r.Header.Set("HX-Target", "article-content")
			w := httptest.NewRecorder()
			h.app.Handler.ServeHTTP(w, r)
			if !strings.Contains(w.Body.String(), `<title>`+html.EscapeString(name)+` | About Me</title>`) {
				t.Fatal("fragment title ignored profile", w.Body)
			}
			rss := h.request("GET", "/rss", nil, false)
			requireStatus(t, rss, http.StatusOK)
			if !strings.Contains(rss.Body.String(), html.EscapeString(name)) || !strings.Contains(rss.Body.String(), html.EscapeString(settings.Profile.Bio)) {
				t.Fatal("RSS ignored profile", rss.Body)
			}
		})
	}
}

// TestProfileValidation rejects invalid settings before storage or application services are used.
func TestProfileValidation(t *testing.T) {
	for _, profile := range []Profile{
		{}, {Name: " "}, {Name: strings.Repeat("x", 241)}, {Name: string([]byte{0xff})},
		{Name: "Writer", Bio: strings.Repeat("x", 501)}, {Name: "Writer", Bio: string([]byte{0xff})},
		{Name: "Writer", ImageURL: "javascript:alert(1)"}, {Name: "Writer", ImageURL: "//elsewhere.test/avatar.png"},
		{Name: "Writer", ImageURL: "http://elsewhere.test/avatar.png"}, {Name: "Writer", ImageURL: "https://user:password@example.test/a"},
	} {
		m := &module{access: &Access{}, settings: &Settings{AdminEmail: "admin@example.com", RecentPostCount: 5, Profile: profile}}
		if err := m.Init(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "blog.profile.") {
			t.Fatal("invalid profile accepted", profile, err)
		}
	}
	for _, image := range []string{"", "/images/avatar.png", "https://example.test/avatar.png"} {
		p := Profile{Name: "Example Author", ImageURL: image}
		if err := p.validate(); err != nil {
			t.Fatal(err)
		}
		if image == "" && p.PortraitURL() != "/assets/img/profile-default.svg" {
			t.Fatal("missing default portrait")
		}
	}
}
