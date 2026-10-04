package user

import (
	"context"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/pkg/database"
)

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
