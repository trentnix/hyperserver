package session

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/database"
)

// ExpiredSessionMaintenance is an optional session-provider capability. A batch
// deletes at most limit expired sessions, preserves live sessions, and honors ctx.
type ExpiredSessionMaintenance interface {
	DeleteExpired(ctx context.Context, limit int) (int64, error)
}

// CleanupExpired removes one bounded batch from each selected server-side store.
// It opens maintenance storage without creating tables or requiring signing keys.
// Each store is visited once and closed after its batch. Independent stores are
// still attempted after a failure. Cookie storage needs no server-side cleanup.
func CleanupExpired(ctx context.Context, c *config.Config, limit int) (count int64, err error) {
	if limit < 1 {
		return 0, fmt.Errorf("session cleanup limit must be positive")
	}
	selected := make(map[string]bool)
	for _, name := range c.HTTP.Session.Types {
		selected[strings.ToLower(name)] = true
	}
	for _, name := range slices.Sorted(maps.Keys(selected)) {
		n, batchErr := cleanupStore(ctx, c, name, limit)
		count += n
		if batchErr != nil {
			err = errors.Join(err, fmt.Errorf("clean up session store %q: %w", name, batchErr))
		}
	}
	return count, err
}

func cleanupStore(ctx context.Context, c *config.Config, name string, limit int) (count int64, err error) {
	var options map[string]string
	found := false
	for key, values := range c.HTTP.Session.Stores {
		if strings.EqualFold(key, name) {
			if found {
				return 0, fmt.Errorf("duplicate session store %q", name)
			}
			options, found = values, true
		}
	}
	if !found {
		return 0, fmt.Errorf("selected session store %q is not configured", name)
	}
	enabled, err := config.ProviderEnabled("http.session.stores."+name+".enabled", options["enabled"])
	if err != nil {
		return 0, err
	}
	if !enabled {
		return 0, NewErrStoreDisabled(nil)
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	maintenance, closeStore, err := openMaintenance(ctx, name, options)
	if err != nil {
		return 0, err
	}
	if closeStore != nil {
		defer func() { err = errors.Join(err, closeStore()) }()
	}
	if maintenance == nil {
		return 0, nil
	}
	return maintenance.DeleteExpired(ctx, limit)
}

func openMaintenance(ctx context.Context, name string, options map[string]string) (ExpiredSessionMaintenance, func() error, error) {
	switch name {
	case strings.ToLower(cookieStoreName):
		return nil, nil, nil
	case strings.ToLower(sqliteStoreName):
		store, err := openSQLiteMaintenance(ctx, options)
		if err != nil {
			return nil, nil, err
		}
		return store, store.Close, nil
	default:
		return nil, nil, fmt.Errorf("unknown session store %q", name)
	}
}

func openSQLiteMaintenance(ctx context.Context, options map[string]string) (*SQLiteStore, error) {
	if strings.TrimSpace(options["connection"]) == "" || strings.TrimSpace(options["sessiontable"]) == "" {
		return nil, ErrSQLiteStoreNotConfigured
	}
	db, err := database.Setup(sqliteDriver, options["connection"])
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return &SQLiteStore{db: db, tableName: options["sessiontable"]}, nil
}

// DeleteExpired removes at most limit expired sessions and returns the deleted count.
// Expiration is checked against the start of the current second, so cleanup can
// leave recently expired rows for the next batch. It does not change live sessions.
func (s *SQLiteStore) DeleteExpired(ctx context.Context, limit int) (int64, error) {
	return DeleteExpiredSQLiteSessions(ctx, s.db, s.tableName, limit)
}

// DeleteExpiredSQLiteSessions deletes at most limit expired sessions and returns
// the deleted count. Like DeleteExpired, it leaves the current second untouched.
// It uses a caller-owned pool.
// The session table must already exist. It does not initialize or close the pool.
func DeleteExpiredSQLiteSessions(ctx context.Context, db *sqlx.DB, table string, limit int) (int64, error) {
	if limit < 1 {
		return 0, fmt.Errorf("session cleanup limit must be positive")
	}
	if db == nil {
		return 0, ErrDatabaseNotConfigured
	}
	if table == "" {
		return 0, ErrSQLiteStoreNotConfigured
	}

	name := `"` + strings.ReplaceAll(table, `"`, `""`) + `"`
	query := fmt.Sprintf(`DELETE FROM %s WHERE id IN (
		SELECT id FROM %s WHERE julianday(expires_at) < julianday(?)
		ORDER BY julianday(expires_at), id LIMIT ?
	)`, name, name)
	result, err := db.ExecContext(ctx, query, time.Now().UTC().Truncate(time.Second), limit)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
