package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// ResolvePaths anchors application file paths without changing the process directory.
// Call before opening services. WorkingDirectory is the resource base, not a chdir
// instruction. GetConfig defaults it to the directory containing the selected file.
// Programmatic configurations default to the current directory at this call.
func (c *Config) ResolvePaths() error {
	base, err := filepath.Abs(c.App.WorkingDirectory)
	if err != nil {
		return err
	}
	info, err := os.Stat(base)
	if err != nil {
		return fmt.Errorf("app.workingDirectory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("app.workingDirectory must name a directory")
	}
	c.App.WorkingDirectory = base
	c.HTTP.TLS.Certificate = c.FilePath(c.HTTP.TLS.Certificate)
	c.HTTP.TLS.Key = c.FilePath(c.HTTP.TLS.Key)
	return nil
}

// FilePath resolves a local filename against the application's resource base.
// Empty and absolute paths are preserved. ResolvePaths must run first.
// Connection strings belong to their storage providers and must not use FilePath.
func (c Config) FilePath(name string) string {
	if name == "" || filepath.IsAbs(name) {
		return name
	}
	return filepath.Join(c.App.WorkingDirectory, name)
}
