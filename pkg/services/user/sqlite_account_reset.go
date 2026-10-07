package user

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/trentnix/hyperserver/pkg/database"
)

// ResetPassword consumes a validated reset token and updates the password in one
// transaction. The caller must check the signature before supplying authorization.
func (s *SQLiteAccountRepository) ResetPassword(ctx context.Context, u *User, authorization ResetAuthorization, passwordHash string) error {
	if u == nil || u.ID == "" {
		return NewErrUserNotSpecified(nil)
	}
	if s.db == nil {
		return database.NewErrDatabaseUnavailable(nil)
	}
	if passwordHash == "" {
		return errors.New("a password hash is required")
	}
	if authorization.AccountID != u.ID || authorization.TokenHash == "" || authorization.ExpiresAt.IsZero() {
		return NewErrToken(errors.New("invalid reset authorization"))
	}

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := consumeStoredToken(ctx, tx, authorization.AccountID, authorization.TokenHash, resetTokenType, authorization.ExpiresAt); err != nil {
		return err
	}
	now := time.Now()

	// Only update the password. Do not overwrite unrelated account changes with
	// the snapshot used for form validation or the previous-password check.
	var updated User
	err = tx.GetContext(ctx, &updated, `UPDATE `+userTableName+` SET password = ?, updated_at = ?, session_version = session_version + 1
		WHERE id = ? AND password = ? AND registration_auth_type = ?
		RETURNING id, email, verified, verification_required, created_at, updated_at, registration_auth_type, password, session_version`,
		passwordHash, now, u.ID, u.Password, u.RegistrationAuthType)
	if errors.Is(err, sql.ErrNoRows) {
		return NewErrToken(errors.New("account changed or no longer exists"))
	}
	if err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	*u = updated
	return nil
}
