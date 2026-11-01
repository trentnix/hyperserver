package backup

import (
	"context"
	"database/sql"
	"errors"
	"net/url"

	dbsqlite "github.com/trentnix/hyperserver/pkg/database/sqlite"
)

// openDatabase never creates a missing source or changes its schema.
func openDatabase(name string) (*sql.DB, error) {
	uri := &url.URL{Scheme: "file", Path: name, RawQuery: "mode=ro"}
	return dbsqlite.OpenExisting(uri.String())
}

func checkDatabase(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return err
	}
	ok := false
	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil {
			rows.Close()
			return err
		}
		if result != "ok" {
			rows.Close()
			return errors.New("database integrity check failed")
		}
		ok = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("database integrity check returned no result")
	}
	rows, err = db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return errors.New("database foreign key check failed")
	}
	return rows.Err()
}

func snapshot(ctx context.Context, source, destination string) error {
	db, err := openDatabase(source)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := checkDatabase(ctx, db); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, "VACUUM INTO ?", destination)
	return err
}
