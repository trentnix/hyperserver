package database

import (
	"net/url"
	"path/filepath"
	"strings"
)

// ResolveConnection anchors SQLite file connections to directory. Other drivers'
// connection strings and SQLite temporary and in-memory databases are unchanged.
// SQLite URI options are preserved. An empty directory uses the current directory.
func ResolveConnection(driver, connection, directory string) (string, error) {
	if driver != "sqlite3" || connection == "" {
		return connection, nil
	}

	name, query, hasQuery := strings.Cut(connection, "?")
	if name == "" {
		return connection, nil
	}
	options, err := url.ParseQuery(query)
	if err != nil {
		return "", err
	}
	if name == ":memory:" || name == "file::memory:" || (strings.HasPrefix(name, "file:") && options.Get("mode") == "memory") {
		return connection, nil
	}

	isURI := strings.HasPrefix(name, "file:")
	if isURI {
		uri, err := url.Parse(name)
		if err != nil {
			return "", err
		}
		// Leave absolute URIs and URI authorities to SQLite's own validation.
		if uri.Host != "" || filepath.IsAbs(uri.Path) {
			return connection, nil
		}
		name, err = url.PathUnescape(uri.Opaque)
		if err != nil {
			return "", err
		}
		if name == "" || name == ":memory:" {
			return connection, nil
		}
	}

	if !filepath.IsAbs(name) {
		name = filepath.Join(directory, name)
	}
	name, err = filepath.Abs(name)
	if err != nil {
		return "", err
	}

	if isURI {
		name = (&url.URL{Scheme: "file", Path: filepath.ToSlash(name)}).String()
	}
	if hasQuery {
		name += "?" + query
	}
	return name, nil
}
