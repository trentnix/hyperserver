package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type testModuleConfig struct {
	Title   string `yaml:"title"`
	Enabled bool   `yaml:"enabled"`
	Storage struct {
		Limit int `yaml:"limit"`
	} `yaml:"storage"`
}

func TestLoadModuleFileDefaults(t *testing.T) {
	for _, path := range []string{"", filepath.Join(t.TempDir(), "missing.yaml"), filepath.Join(t.TempDir(), "absent", "module.yaml")} {
		cfg := testModuleConfig{Title: "Default", Enabled: true}
		want := cfg
		found, err := LoadModuleFile(path, &cfg)
		if err != nil || found || cfg != want {
			t.Fatalf("missing file %q: found=%v, config=%+v, error=%v", path, found, cfg, err)
		}
	}
	for _, input := range []string{"", "# Defaults only\n", "{}"} {
		path := filepath.Join(t.TempDir(), "module.yaml")
		if err := os.WriteFile(path, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		cfg := testModuleConfig{Title: "Default", Enabled: true}
		want := cfg
		found, err := LoadModuleFile(path, &cfg)
		if err != nil || !found || cfg != want {
			t.Fatalf("empty file: found=%v, config=%+v, error=%v", found, cfg, err)
		}
	}
}

func TestLoadModuleFileStrictDecoding(t *testing.T) {
	for name, input := range map[string]string{
		"syntax":             "title: [secret-marker",
		"unknown field":      "titel: secret-marker",
		"unknown nested":     "storage: {unknown: secret-marker}",
		"duplicate key":      "title: first\ntitle: secret-marker",
		"duplicate nested":   "storage: {limit: 1, limit: 2}",
		"boolean":            "enabled: secret-marker",
		"integer":            "storage: {limit: secret-marker}",
		"multiple documents": "{}\n---\ntitle: secret-marker",
		"empty second":       "{}\n---\n",
		"scalar":             "secret-marker",
		"list":               "[secret-marker]",
		"null":               "null",
		"recursive alias":    "storage: &loop {limit: *loop}",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "module.yaml")
			if err := os.WriteFile(path, []byte(input), 0600); err != nil {
				t.Fatal(err)
			}
			var cfg testModuleConfig
			found, err := LoadModuleFile(path, &cfg)
			if !found || err == nil || !strings.Contains(err.Error(), path) {
				t.Fatalf("found=%v, error=%v", found, err)
			}
			if strings.Contains(err.Error(), "secret-marker") {
				t.Fatal("configuration error exposed a value")
			}
		})
	}
}

func TestLoadModuleFileReadFailures(t *testing.T) {
	var cfg testModuleConfig
	if _, err := LoadModuleFile(t.TempDir(), &cfg); err == nil {
		t.Fatal("directory accepted as a file")
	}
	path := filepath.Join(t.TempDir(), "module.yaml")
	if err := os.WriteFile(path, []byte("title: unreadable"), 0000); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() == 0 {
		t.Skip("root can read files without read permission")
	}
	if _, err := LoadModuleFile(path, &cfg); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("read failure hidden: %v", err)
	}
}

func TestLoadModuleFileDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
	}{
		{"unknown field", "title: secret-marker\ntitel: secret-marker\n", "line 2: module configuration: unknown field"},
		{"unknown nested field", "storage:\n  secret-marker: hidden\n", "line 2: module configuration: unknown field"},
		{"duplicate key", "title: secret-marker\ntitle: hidden\n", "line 2: module configuration: duplicate key"},
		{"duplicate secret key", "secret-marker: first\nsecret-marker: second\n", "line 2: module configuration: duplicate key"},
		{"wrong type", "title: secret-marker\nenabled: secret-marker\n", "line 2: module configuration: invalid value"},
		{"wrong nested type", "storage:\n  limit: secret-marker\n", "line 2: module configuration: invalid value"},
		// The parser reports the preceding context line for these syntax errors.
		{"syntax", "title: secret-marker\nenabled: ]\n", "line 1: module configuration: invalid YAML"},
		{"syntax in second document", "{}\n---\nenabled: ]\n", "line 2: module configuration: invalid YAML"},
		{"unknown anchor", "title: *secret-marker\n", "module configuration: invalid YAML"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tc.input), 0600); err != nil {
				t.Fatal(err)
			}
			var cfg testModuleConfig
			_, err := LoadModuleFile(path, &cfg)
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), path) {
				t.Fatalf("got %v, want path and %q", err, tc.want)
			}
			if strings.Contains(err.Error(), "secret-marker") {
				t.Fatalf("diagnostic exposed configuration content: %v", err)
			}
		})
	}
}

func TestModuleConfigErrorUnknownFormats(t *testing.T) {
	for _, err := range []error{
		errors.New("custom decoder rejected secret-marker"),
		errors.New("yaml: line secret-marker: bad value"),
		errors.New("yaml: line -1: secret-marker"),
		errors.New("yaml: line 3: secret-marker"),
	} {
		got := moduleConfigError("config.yaml", err, "invalid value").Error()
		if !strings.Contains(got, "invalid value") || strings.Contains(got, "secret-marker") {
			t.Fatalf("unsafe fallback diagnostic: %s", got)
		}
	}
}

func TestLoadModuleFileDestination(t *testing.T) {
	var typedNil *testModuleConfig
	for _, destination := range []any{nil, typedNil, testModuleConfig{}, new(string), new(map[string]string)} {
		if _, err := LoadModuleFile("", destination); err == nil {
			t.Fatalf("invalid destination %T accepted", destination)
		}
	}
}

func TestLoadModuleFileUsesOnlySelectedPath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "module.yaml"), []byte("title: Parent"), 0600); err != nil {
		t.Fatal(err)
	}
	working := filepath.Join(root, "app")
	if err := os.Mkdir(working, 0700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(working)
	t.Setenv("HYPERSERVER_TITLE", "Environment")
	cfg := testModuleConfig{Title: "Default"}
	if found, err := LoadModuleFile("module.yaml", &cfg); found || err != nil || cfg.Title != "Default" {
		t.Fatalf("unselected source applied: found=%v, config=%+v, error=%v", found, cfg, err)
	}
	if found, err := LoadModuleFile("../module.yaml", &cfg); !found || err != nil || cfg.Title != "Parent" {
		t.Fatalf("explicit relative path failed: found=%v, config=%+v, error=%v", found, cfg, err)
	}
}

func TestLoadModuleFileIsolation(t *testing.T) {
	for _, title := range []string{"First", "Second"} {
		t.Run(title, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "module.yaml")
			if err := os.WriteFile(path, []byte("title: "+title+"\nenabled: false\nstorage: {limit: 7}\n"), 0600); err != nil {
				t.Fatal(err)
			}
			cfg := testModuleConfig{Title: "Default", Enabled: true}
			found, err := LoadModuleFile(path, &cfg)
			if err != nil || !found || cfg.Title != title || cfg.Enabled || cfg.Storage.Limit != 7 {
				t.Fatalf("found=%v, config=%+v, error=%v", found, cfg, err)
			}

			// The same loader accepts a different module's settings without registration.
			otherPath := filepath.Join(t.TempDir(), "other.yaml")
			if err := os.WriteFile(otherPath, []byte("tags: [one, two]\n"), 0600); err != nil {
				t.Fatal(err)
			}
			var other struct {
				Tags []string `yaml:"tags"`
			}
			if _, err := LoadModuleFile(otherPath, &other); err != nil || !reflect.DeepEqual(other.Tags, []string{"one", "two"}) || cfg.Title != title {
				t.Fatalf("module settings were not independent: %+v, %v", other, err)
			}
		})
	}
}
