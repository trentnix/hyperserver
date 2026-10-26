package blog

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestDefaultProfileAssets checks neutral defaults and the removal of personal assets.
func TestDefaultProfileAssets(t *testing.T) {
	h := newHarness(t)
	body := h.request("GET", "/about", nil, false).Body.String()
	if !strings.Contains(body, `<link rel="icon" href="/assets/img/favicon.svg">`) || !strings.Contains(body, `src="/assets/img/profile-default.svg"`) {
		t.Fatal("missing neutral profile assets")
	}
	for _, path := range []string{"/assets/img/favicon.svg", "/assets/img/profile-default.svg"} {
		w := h.request("GET", path, nil, false)
		requireStatus(t, w, http.StatusOK)
		if !strings.Contains(w.Header().Get("Content-Type"), "image/svg+xml") {
			t.Fatal("incorrect SVG content type")
		}
	}
	for _, path := range []string{"/assets/img/favicon.ico", "/assets/img/trent-nix-small.jpg", "/local-assets/site-icon.ico"} {
		requireStatus(t, h.request("GET", path, nil, false), http.StatusNotFound)
	}
	if body := h.request("GET", "/assets/img/", nil, false).Body.String(); strings.Contains(body, "trent-nix-small") || strings.Contains(body, "favicon.ico") {
		t.Fatal("personal assets remain in the bundle")
	}
}

// TestLocalAssets checks configured files, HTTP file semantics, and filesystem confinement.
func TestLocalAssets(t *testing.T) {
	h := newHarness(t)
	cfg := *h.app.Config
	settings := *h.blog.settings
	if err := h.app.Shutdown(); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(cfg.App.WorkingDirectory, "assets")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	data := testPNG(t)
	for _, name := range []string{"icon.png", ".private.png"} {
		if err := os.WriteFile(filepath.Join(directory, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(directory, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(cfg.App.WorkingDirectory, "private.png")
	if err := os.WriteFile(outside, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(directory, "escape.png")); err != nil {
		t.Fatal(err)
	}
	settings.AssetsDirectory = "assets"
	yamlData, err := yaml.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.App.WorkingDirectory, "modules/blog/config.yaml"), yamlData, 0600); err != nil {
		t.Fatal(err)
	}
	h = startHarness(t, cfg)
	profile := h.blog.profile.Current()
	profile.ImageURL, profile.FaviconURL = "/local-assets/icon.png", "/local-assets/icon.png"
	if err := h.blog.writeProfile(t.Context(), profile); err != nil {
		t.Fatal(err)
	}
	page := h.request("GET", "/about", nil, false).Body.String()
	if !strings.Contains(page, `<link rel="icon" href="/local-assets/icon.png">`) || !strings.Contains(page, `src="/local-assets/icon.png"`) {
		t.Fatal("local profile configuration not rendered")
	}
	w := h.request("GET", "/local-assets/icon.png", nil, false)
	requireStatus(t, w, http.StatusOK)
	if !bytes.Equal(w.Body.Bytes(), data) || w.Header().Get("Content-Type") != "image/png" || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatal("incorrect local asset response", w.Header())
	}
	modified := w.Header().Get("Last-Modified")
	for _, tc := range []struct {
		method, header, value string
		status                int
	}{
		{"HEAD", "", "", http.StatusOK},
		{"GET", "Range", "bytes=0-3", http.StatusPartialContent},
		{"GET", "If-Modified-Since", modified, http.StatusNotModified},
	} {
		r := httptest.NewRequest(tc.method, "/local-assets/icon.png", nil)
		if tc.header != "" {
			r.Header.Set(tc.header, tc.value)
		}
		w := httptest.NewRecorder()
		h.app.Handler.ServeHTTP(w, r)
		requireStatus(t, w, tc.status)
		if tc.status == http.StatusPartialContent && !bytes.Equal(w.Body.Bytes(), data[:4]) {
			t.Fatal("range ignored")
		}
		if (tc.method == "HEAD" || tc.status == http.StatusNotModified) && w.Body.Len() != 0 {
			t.Fatal("unexpected response body")
		}
	}
	for _, path := range []string{"/local-assets/", "/local-assets/nested", "/local-assets/nested/file.png", "/local-assets/.private.png", "/local-assets/missing.png", "/local-assets/escape.png", "/local-assets/%2e%2e%2fprivate.png", "/local-assets/%2Fetc%2Fpasswd"} {
		requireStatus(t, h.request("GET", path, nil, false), http.StatusNotFound)
	}
	root := h.blog.localAssets
	if err := h.app.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if file, err := root.Open("icon.png"); err == nil {
		file.Close()
		t.Fatal("shutdown left the assets root open")
	}
}

// TestOpenLocalAssets makes optional configuration explicit and rejects invalid paths.
func TestOpenLocalAssets(t *testing.T) {
	directory := t.TempDir()
	if root, err := openLocalAssets("", directory); err != nil || root != nil {
		t.Fatal("empty configuration must disable local assets", err)
	}
	for _, path := range []string{"missing", "not-a-directory"} {
		if path == "not-a-directory" {
			if err := os.WriteFile(filepath.Join(directory, path), []byte("file"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if root, err := openLocalAssets(path, directory); err == nil {
			root.Close()
			t.Fatal("invalid asset directory accepted")
		}
	}
	root, err := openLocalAssets(directory, t.TempDir())
	if err != nil {
		t.Fatal("absolute directory failed", err)
	}
	root.Close()
}

// TestLocalAssetsInitializationFailure rejects a configured directory that cannot be opened.
func TestLocalAssetsInitializationFailure(t *testing.T) {
	h := newHarness(t)
	settings := *h.blog.settings
	settings.AssetsDirectory = "missing-assets"
	m := &module{settings: &settings, access: &Access{}}
	if err := m.Init(t.Context(), h.blog.app); err == nil || !strings.Contains(err.Error(), "open blog assets") {
		t.Fatal("invalid configured assets did not fail initialization", err)
	}
	if m.access.emailKey != "" || m.close != nil || m.localAssets != nil {
		t.Fatal("failed initialization published runtime state")
	}
	if _, err := h.blog.pageStore.ListPages(t.Context(), true); err != nil {
		t.Fatal("failed initialization closed another application's storage", err)
	}
}
