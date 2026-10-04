package user

import (
	"context"
	"errors"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/pkg/database"
)

// ChangePassword stores a validated password hash and invalidates all existing
// authenticated sessions. A concurrent account change rejects the operation.
// The receiver changes only after the database update succeeds.
func (u *User) ChangePassword(ctx context.Context, db *sqlx.DB, passwordHash string) error {
	if u == nil || u.ID == "" {
		return NewErrUserNotSpecified(nil)
	}
	if db == nil {
		return database.NewErrDatabaseUnavailable(nil)
	}
	if passwordHash == "" {
		return errors.New("a password hash is required")
	}

	now := time.Now()
	result, err := db.ExecContext(ctx, `UPDATE user
		SET password = ?, updated_at = ?, session_version = session_version + 1
		WHERE id = ? AND password = ? AND registration_auth_type = ? AND session_version = ?`,
		passwordHash, now, u.ID, u.Password, u.RegistrationAuthType, u.SessionVersion)
	if err != nil {
		return err
	}

	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("account changed or no longer exists")
	}

	u.Password = passwordHash
	u.UpdatedAt = now
	u.SessionVersion++
	return nil
}
