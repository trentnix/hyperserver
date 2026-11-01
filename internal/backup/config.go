// Package backup creates ordinary ZIP archives for offline blog recovery.
package backup

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/modules/blog"
)

// resource records where an archived file or directory came from.
// Roles identifies database snapshots. Shared databases are archived once.
type resource struct {
	Path   string
	Source string
	Roles  []string `json:",omitempty"`
}

func loadSettings(directory string) (blog.Settings, error) {
	var settings blog.Settings
	found, err := config.LoadModuleFile(filepath.Join(directory, "modules", "blog", "config.yaml"), &settings)
	if err == nil && !found {
		err = errors.New("modules/blog/config.yaml is required")
	}
	return settings, err
}

// resources selects the blog's configured persistent data without changing configuration.
func resources(ctx context.Context, cfg config.Config) ([]resource, error) {
	settings, err := loadSettings(cfg.App.WorkingDirectory)
	if err != nil {
		return nil, err
	}
	if settings.Storage.Provider != "sqlite" || !cfg.Auth.Enabled ||
		(cfg.Auth.AccountStorage.Provider != "" && cfg.Auth.AccountStorage.Provider != "sqlite") {
		return nil, errors.New("backup requires SQLite blog and account storage")
	}
	var files []resource
	addDatabase := func(connection, role string) error {
		source, err := databasePath(connection, cfg.App.WorkingDirectory)
		if err != nil {
			return fmt.Errorf("%s: %w", role, err)
		}
		info, err := os.Stat(source)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("database must be a regular file")
		}
		for i := range files {
			other, err := os.Stat(files[i].Source)
			if err != nil {
				return err
			}
			if os.SameFile(info, other) {
				files[i].Roles = append(files[i].Roles, role)
				return nil
			}
		}
		files = append(files, resource{Path: "databases/" + role + ".db", Source: source, Roles: []string{role}})
		return nil
	}
	if err := addDatabase(settings.Storage.Connection, "blog"); err != nil {
		return nil, err
	}
	if err := addDatabase(cfg.Auth.AccountStorage.Options["connection"], "accounts"); err != nil {
		return nil, err
	}
	if len(cfg.HTTP.Session.Types) != 1 {
		return nil, errors.New("backup requires one SQLite session store")
	}
	for _, name := range cfg.HTTP.Session.Types {
		if !strings.EqualFold(name, "sqlitestore") {
			return nil, errors.New("backup requires one SQLite session store")
		}
	}
	var options map[string]string
	for key, value := range cfg.HTTP.Session.Stores {
		if strings.EqualFold(key, "sqlitestore") {
			if options != nil {
				return nil, errors.New("duplicate SQLite session store")
			}
			options = value
		}
	}
	enabled, err := config.ProviderEnabled("session store", options["enabled"])
	if err != nil || !enabled {
		return nil, errors.New("SQLite session store must be enabled")
	}
	if err := addDatabase(options["connection"], "sessions"); err != nil {
		return nil, err
	}
	files = append(files,
		resource{Path: "config.yaml", Source: resolve(cfg.App.WorkingDirectory, "config.yaml")},
		resource{Path: "modules/blog/config.yaml", Source: resolve(cfg.App.WorkingDirectory, "modules/blog/config.yaml")},
	)
	if settings.UploadsDirectory == "" {
		return nil, errors.New("uploadsDirectory is required")
	}
	for _, item := range []struct{ name, target string }{
		{settings.UploadsDirectory, "uploads"},
		{settings.AssetsDirectory, "assets"},
	} {
		if item.name == "" {
			continue
		}
		source := resolve(cfg.App.WorkingDirectory, item.name)
		info, err := os.Lstat(source)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, errors.New("uploads and assets must be directories, not symlinks")
		}
		// Do not accidentally include databases, configuration, or the application itself.
		if err := filepath.WalkDir(source, func(name string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.IsDir() && !info.Mode().IsRegular() {
				return errors.New("backup refuses symlinks and special files")
			}
			for _, file := range files {
				other, err := os.Stat(file.Source)
				if err != nil {
					return err
				}
				if os.SameFile(info, other) {
					return errors.New("backup resource directories must not overlap other resources")
				}
			}
			return nil
		}); err != nil {
			return nil, err
		}
		files = append(files, resource{Path: item.target, Source: source})
	}
	if cfg.HTTP.TLS.Enabled {
		files = append(files,
			resource{Path: "tls/certificate.pem", Source: resolve(cfg.App.WorkingDirectory, cfg.HTTP.TLS.Certificate)},
			resource{Path: "tls/key.pem", Source: resolve(cfg.App.WorkingDirectory, cfg.HTTP.TLS.Key)},
		)
	}
	return files, nil
}

func resolve(directory, name string) string {
	if filepath.IsAbs(name) {
		return filepath.Clean(name)
	}
	return filepath.Join(directory, name)
}

func within(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	return err == nil && filepath.IsLocal(relative)
}

// databasePath rejects temporary databases and URI behaviors that can bypass locking.
func databasePath(connection, directory string) (string, error) {
	name, query, _ := strings.Cut(connection, "?")
	options, err := url.ParseQuery(query)
	if err != nil || name == "" || name == ":memory:" || options.Get("mode") == "memory" {
		return "", errors.New("backup requires a local SQLite database file")
	}
	for key := range options {
		switch key {
		case "mode", "cache", "_journal", "_journal_mode", "_fk", "_foreign_keys", "_timeout", "_busy_timeout", "_sync", "_synchronous", "_txlock":
		default:
			return "", fmt.Errorf("backup does not support SQLite option %q", key)
		}
		if len(options[key]) != 1 {
			return "", errors.New("duplicate SQLite option")
		}
	}
	if mode := options.Get("mode"); mode != "" && mode != "ro" && mode != "rw" && mode != "rwc" {
		return "", errors.New("unsupported SQLite mode")
	}
	if strings.HasPrefix(name, "file:") {
		uri, err := url.Parse(name)
		if err != nil || uri.Host != "" || uri.Fragment != "" {
			return "", errors.New("unsupported SQLite file URI")
		}
		name = uri.Path
		if uri.Opaque != "" {
			name, err = url.PathUnescape(uri.Opaque)
			if err != nil {
				return "", errors.New("invalid SQLite file URI")
			}
		}
	}
	if name == "" || name == ":memory:" {
		return "", errors.New("backup requires a local SQLite database file")
	}
	return resolve(directory, name), nil
}
