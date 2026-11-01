package blog

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/internal/maintenance"
	"github.com/trentnix/hyperserver/pkg/database"
	dbsqlite "github.com/trentnix/hyperserver/pkg/database/sqlite"
)

// AdoptSchema records version 1 for a matching unversioned blog. It must run
// offline after a backup. It never upgrades tables or changes content or images.
func AdoptSchema(ctx context.Context, directory string) error {
	lock, err := maintenance.Lock(directory)
	if err != nil {
		return err
	}
	defer lock.Close()
	var settings Settings
	found, err := config.LoadModuleFile(filepath.Join(directory, "modules", "blog", "config.yaml"), &settings)
	if err != nil {
		return err
	}
	if !found || settings.Storage.Provider != "sqlite" {
		return fmt.Errorf("schema adoption requires modules/blog/config.yaml with SQLite blog storage")
	}
	connection, err := database.ResolveConnection("sqlite3", settings.Storage.Connection, directory)
	if err != nil {
		return err
	}
	db, err := dbsqlite.OpenExisting(connection)
	if err != nil {
		return err
	}
	defer db.Close()
	return adoptBlogSchema(ctx, db)
}

// adoptBlogSchema validates before recording metadata in the same transaction.
// Repeating adoption is harmless, but a versioned database is never relabeled.
func adoptBlogSchema(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE name='blog_schema_version' COLLATE NOCASE)`).Scan(&exists); err != nil {
		return err
	}
	if exists {
		if err := checkBlogVersion(ctx, tx); err != nil {
			return err
		}
	}
	if err := validateBlogSchema(ctx, tx); err != nil {
		return err
	}
	var violations int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_check WHERE lower("table") GLOB 'blog_*'`).Scan(&violations); err != nil {
		return err
	}
	if violations != 0 {
		return fmt.Errorf("blog schema adoption refused: %d broken foreign-key references", violations)
	}
	if !exists {
		if err := recordBlogVersion(ctx, tx); err != nil {
			return err
		}
	}
	return tx.Commit()
}
