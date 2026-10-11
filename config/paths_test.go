package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigPathsUseSelectedFileDirectory(t *testing.T) {
	cleanConfigEnvironment(t)
	launch := t.TempDir()
	t.Chdir(launch)
	directory := filepath.Join(launch, "config")
	if err := os.MkdirAll(filepath.Join(directory, "data"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, override := range []string{"", "data", directory} {
		if err := os.WriteFile(filepath.Join(directory, "config.yaml"), []byte("app:\n  workingDirectory: \""+filepath.ToSlash(override)+"\"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := GetConfig()
		if err != nil {
			t.Fatal(err)
		}
		cfg.HTTP.TLS.Certificate, cfg.HTTP.TLS.Key = "cert.pem", "key.pem"
		if err := cfg.ResolvePaths(); err != nil {
			t.Fatal(err)
		}
		want := directory
		if override == "data" {
			want = filepath.Join(directory, "data")
		}
		if cfg.App.WorkingDirectory != want || cfg.HTTP.TLS.Certificate != filepath.Join(want, "cert.pem") || cfg.HTTP.TLS.Key != filepath.Join(want, "key.pem") {
			t.Fatalf("resolved paths = %+v", cfg.HTTP.TLS)
		}
		if cfg.FilePath("") != "" || cfg.FilePath(directory) != directory {
			t.Fatal("empty or absolute path changed")
		}
		if err := cfg.ResolvePaths(); err != nil || cfg.HTTP.TLS.Certificate != filepath.Join(want, "cert.pem") {
			t.Fatal("repeated resolution changed paths:", err)
		}
		if cwd, err := os.Getwd(); err != nil || cwd != launch {
			t.Fatalf("configuration changed process directory: %q %v", cwd, err)
		}
	}
}

func TestResolvePathsRejectsMissingDirectoryAndFiles(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, base := range []string{filepath.Join(dir, "missing"), file} {
		cfg := Config{App: AppConfig{WorkingDirectory: base}}
		if err := cfg.ResolvePaths(); err == nil {
			t.Fatalf("accepted resource base %q", base)
		}
	}
}
