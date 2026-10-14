package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	sqlitedb "github.com/trentnix/hyperserver/pkg/database/sqlite"
	"github.com/trentnix/hyperserver/pkg/services/session"
)

// OpenMaintenance opens an existing session database without requiring signing
// keys. It does not create files or change the schema. Application setup must
// resolve connection's path. The caller must call the returned close function
// after the batch.
// Failed setup closes any pool it opened.
func OpenMaintenance(ctx context.Context, connection, table string) (session.ExpiredSessionMaintenance, func() error, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(connection) == "" || strings.TrimSpace(table) == "" {
		return nil, nil, ErrSQLiteStoreNotConfigured
	}

	db, err := sqlitedb.OpenExisting(connection)
	if err != nil {
		return nil, nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		return nil, nil, errors.Join(err, db.Close())
	}
	return &SQLiteStore{db: db, tableName: table}, db.Close, nil
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
func DeleteExpiredSQLiteSessions(ctx context.Context, db *sql.DB, table string, limit int) (int64, error) {
	if limit < 1 {
		return 0, fmt.Errorf("session cleanup limit must be positive")
	}
	if db == nil {
		return 0, ErrDatabaseNotConfigured
	}
	if table == "" {
		return 0, ErrSQLiteStoreNotConfigured
	}

	name := quoteIdentifier(table)
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
