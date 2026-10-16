// Package sqlite opens SQLite pools with the built-in providers' connection policy.
package sqlite

import (
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	_ "github.com/mattn/go-sqlite3"
	"github.com/trentnix/hyperserver/pkg/database"
	hs_errors "github.com/trentnix/hyperserver/pkg/errors"
)

// Open opens an owned SQLite pool without pinging it or preparing a schema.
// The caller must close the pool. One open and idle connection serializes access
// and keeps private in-memory databases alive between operations.
// Foreign keys are required. The default lock wait is 5000 milliseconds, which
// connection's _busy_timeout or _timeout option can override. A zero timeout
// disables waiting. Cancellation during a lock wait can be delayed until the
// timeout and return a lock error instead of the context error.
// Settings are applied by the driver to every new or replacement connection.
func Open(connection string) (*sql.DB, error) {
	dsn, err := connectionString(connection)
	if err != nil {
		return nil, hs_errors.WithCause(database.ErrDatabaseConfiguration, err)
	}
	db, err := database.Setup("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return db, nil
}

// OpenExisting opens a pool for an existing database file with Open's connection
// policy. It disables file creation, including on replacement connections.
// Read-only mode is preserved. In-memory databases are not supported.
// Like Open, it does not connect until first use. The caller must ping and close it.
func OpenExisting(connection string) (*sql.DB, error) {
	dsn, err := connectionString(connection)
	if err != nil {
		return nil, hs_errors.WithCause(database.ErrDatabaseConfiguration, err)
	}
	path, query, _ := strings.Cut(dsn, "?")
	options, _ := url.ParseQuery(query) // connectionString validated the options.
	mode := options.Get("mode")
	if len(options["mode"]) > 1 || (mode != "" && mode != "rw" && mode != "rwc" && mode != "ro") {
		return nil, hs_errors.WithCause(database.ErrDatabaseConfiguration, fmt.Errorf("SQLite maintenance requires a file connection with mode rw, rwc, or ro"))
	}

	var uri *url.URL
	if strings.HasPrefix(path, "file:") {
		uri, err = url.Parse(path)
	} else if path != ":memory:" {
		path, err = filepath.Abs(path)
		uri = &url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	}
	if err != nil || uri == nil {
		return nil, hs_errors.WithCause(database.ErrDatabaseConfiguration, fmt.Errorf("SQLite maintenance requires a database file"))
	}
	name := uri.Path
	if uri.Opaque != "" {
		name, err = url.PathUnescape(uri.Opaque)
	}
	if err != nil || name == "" || name == ":memory:" {
		return nil, hs_errors.WithCause(database.ErrDatabaseConfiguration, fmt.Errorf("SQLite maintenance requires a database file"))
	}

	if mode != "ro" {
		options.Set("mode", "rw")
	}
	uri.RawQuery = options.Encode()
	return Open(uri.String())
}

func connectionString(connection string) (string, error) {
	path, query, _ := strings.Cut(connection, "?")
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("SQLite connection must specify a file or :memory:")
	}
	options, err := url.ParseQuery(query)
	if err != nil {
		return "", fmt.Errorf("invalid SQLite connection options")
	}
	for _, key := range []string{"_foreign_keys", "_fk"} {
		values, exists := options[key]
		if !exists {
			continue
		}
		if len(values) != 1 {
			return "", fmt.Errorf("SQLite %s must be specified once", key)
		}
		switch strings.ToLower(values[0]) {
		case "1", "true", "yes", "on":
		default:
			return "", fmt.Errorf("SQLite %s must enable foreign keys", key)
		}
	}
	options.Del("_fk")
	options.Set("_foreign_keys", "on")

	timeout, specified := int64(5000), false
	for _, key := range []string{"_busy_timeout", "_timeout"} {
		values, exists := options[key]
		if !exists {
			continue
		}
		if len(values) != 1 {
			return "", fmt.Errorf("SQLite %s must be specified once", key)
		}
		value, err := strconv.ParseInt(values[0], 10, 32)
		if err != nil || value < 0 {
			return "", fmt.Errorf("SQLite %s must be milliseconds between 0 and 2147483647", key)
		}
		if specified && value != timeout {
			return "", fmt.Errorf("SQLite _busy_timeout and _timeout must agree")
		}
		timeout, specified = value, true
	}
	options.Del("_timeout")
	options.Set("_busy_timeout", strconv.FormatInt(timeout, 10))
	return path + "?" + options.Encode(), nil
}
