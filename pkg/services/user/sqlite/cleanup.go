package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/trentnix/hyperserver/pkg/database"
	sqlitedb "github.com/trentnix/hyperserver/pkg/database/sqlite"
	accounts "github.com/trentnix/hyperserver/pkg/services/user"
)

// OpenTokenMaintenance opens existing account storage for maintenance.
// It does not create files or change the schema. Application setup must resolve
// connection's path. The caller must call the returned close function after the batch.
// Failed setup closes any pool it opened.
func OpenTokenMaintenance(ctx context.Context, connection string) (accounts.TokenMaintenance, func() error, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	db, err := sqlitedb.OpenExisting(connection)
	if err != nil {
		return nil, nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		return nil, nil, errors.Join(err, db.Close())
	}
	return NewAccountRepository(db), db.Close, nil
}

// DeleteExpiredTokens removes at most limit expired account tokens and returns
// the deleted count. PrepareDatabase must run first. Tokens that expired during
// the current second can remain until the next batch. Live tokens are unchanged.
func DeleteExpiredTokens(ctx context.Context, db *sql.DB, limit int) (int64, error) {
	if limit < 1 {
		return 0, fmt.Errorf("token cleanup limit must be positive")
	}
	if db == nil {
		return 0, database.ErrDatabaseUnavailable
	}

	result, err := db.ExecContext(ctx, `DELETE FROM usertoken WHERE (user_id, token_hash) IN (
		SELECT user_id, token_hash FROM usertoken WHERE julianday(expires_at) < julianday(?)
		ORDER BY julianday(expires_at), user_id, token_hash LIMIT ?
	)`, time.Now().UTC().Truncate(time.Second), limit)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
