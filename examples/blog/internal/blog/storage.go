package blog

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"github.com/trentnix/hyperserver/config"
	dbsqlite "github.com/trentnix/hyperserver/pkg/database/sqlite"
)

// The version table follows golang-migrate's SQLite format and belongs only to
// blog content. Accounts and sessions can share the file without sharing versions.
const blogVersionSchema = `CREATE TABLE blog_schema_version (version uint64, dirty bool)`

//go:embed migrations/000001_blog.up.sql
var blogSchemaV1 string

// sqliteRepository stores blog content and the site profile.
type sqliteRepository struct {
	db  *sql.DB
	now func() time.Time
}

// openRepository keeps storage selection and schema setup inside the blog.
// On success, the caller owns the returned close function.
func openRepository(ctx context.Context, settings Settings, directory string) (*sqliteRepository, func() error, error) {
	if settings.Storage.Provider != "sqlite" {
		return nil, nil, errors.New("blog.storage.provider must be sqlite")
	}
	db, err := dbsqlite.OpenConfigured(config.DatabaseConfig{Driver: "sqlite3", Connection: settings.Storage.Connection}, directory)
	if err != nil {
		return nil, nil, err
	}
	if err := prepareBlogSchema(ctx, db); err != nil {
		db.Close()
		return nil, nil, err
	}
	return &sqliteRepository{db: db, now: time.Now}, db.Close, nil
}

// prepareBlogSchema bootstraps empty storage. Existing storage must already have
// a clean, supported version. Upgrades and adoption are explicit offline actions.
func prepareBlogSchema(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE lower(name) GLOB 'blog_*'`).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		if _, err := tx.ExecContext(ctx, blogSchemaV1); err != nil {
			return fmt.Errorf("create blog schema: %w", err)
		}
		if err := recordBlogVersion(ctx, tx); err != nil {
			return err
		}
	}
	if err := checkBlogVersion(ctx, tx); err != nil {
		return err
	}
	if err := validateBlogSchema(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

// recordBlogVersion marks an already prepared or validated baseline atomically.
func recordBlogVersion(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, blogVersionSchema+`; INSERT INTO blog_schema_version VALUES (1, false)`); err != nil {
		return fmt.Errorf("record blog schema version: %w", err)
	}
	return nil
}

// checkBlogVersion rejects ambiguous metadata and never repairs a dirty migration.
func checkBlogVersion(ctx context.Context, tx *sql.Tx) error {
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='blog_schema_version' COLLATE NOCASE)`).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("unversioned blog database: stop the server, back up, then run blog-maintenance adopt-schema with the existing configuration")
	}
	var count int
	var version sql.NullInt64
	var dirty sql.NullBool
	if err := tx.QueryRowContext(ctx, `SELECT count(*), min(version), min(dirty) FROM blog_schema_version`).Scan(&count, &version, &dirty); err != nil {
		return fmt.Errorf("read blog schema version: %w", err)
	}
	if count != 1 || !version.Valid || !dirty.Valid {
		return fmt.Errorf("invalid blog schema version: expected one version and dirty flag")
	}
	if dirty.Bool {
		return fmt.Errorf("blog schema version %d is dirty: repair the failed migration or restore a compatible backup before starting", version.Int64)
	}
	if version.Int64 != 1 {
		return fmt.Errorf("unsupported blog schema version %d: this application requires version 1; use compatible migrations or a compatible binary", version.Int64)
	}
	return nil
}
