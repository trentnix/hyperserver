package user

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/database"
)

// TokenMaintenance is an optional account-provider capability, separate from
// AccountRepository. A batch deletes at most limit expired tokens. Implementations
// must honor cancellation, preserve live tokens, and report the deleted count.
type TokenMaintenance interface {
	DeleteExpiredTokens(ctx context.Context, limit int) (int64, error)
}

// OpenSQLiteTokenMaintenance opens existing account storage for maintenance.
// It does not create or migrate tables. The caller must call the returned close
// function after the batch. Failed setup closes any pool it opened.
func OpenSQLiteTokenMaintenance(ctx context.Context, cfg config.DatabaseConfig, options map[string]string) (TokenMaintenance, func() error, error) {
	if err := ValidateSQLiteAccountOptions(options); err != nil {
		return nil, nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if cfg.Driver != "sqlite3" {
		return nil, nil, errors.New("SQLite account maintenance requires the sqlite3 database driver")
	}
	db, err := database.Setup(cfg.Driver, cfg.Connection)
	if err != nil {
		return nil, nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		return nil, nil, errors.Join(err, db.Close())
	}
	return NewSQLiteAccountRepository(db), db.Close, nil
}

// DeleteExpiredTokens removes at most limit expired account tokens and returns
// the deleted count. PrepareDatabase must run first. Tokens that expired during
// the current second can remain until the next batch. Live tokens are unchanged.
func DeleteExpiredTokens(ctx context.Context, db *sqlx.DB, limit int) (int64, error) {
	if limit < 1 {
		return 0, fmt.Errorf("token cleanup limit must be positive")
	}
	if db == nil {
		return 0, database.NewErrDatabaseUnavailable(nil)
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
