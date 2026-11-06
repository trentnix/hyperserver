package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

var _ user.AdministrationRepository = (*AccountRepository)(nil)
var _ user.InvitationRepository = (*AccountRepository)(nil)

func (s *AccountRepository) ListUsers(ctx context.Context) ([]user.User, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+accountColumns+" FROM user WHERE role<>'' ORDER BY email_key")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []user.User
	for rows.Next() {
		u, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, *u)
	}
	return users, rows.Err()
}

func (s *AccountRepository) InitializeRole(ctx context.Context, email, role string) error {
	if role == "" || user.EmailKey(email) == "" {
		return user.ErrAccessDenied
	}
	_, err := s.db.ExecContext(ctx, `UPDATE user SET role=?, updated_at=?
 WHERE email_key=? AND role='' AND invited=0
 AND NOT EXISTS(SELECT 1 FROM user WHERE role<>'')`, role, time.Now(), user.EmailKey(email))
	// This one-time assignment does not change credentials or revoke the first
	// login. Subsequent role changes always use ChangeUserRole.
	return err
}

// authorizeAdministrator acquires the write lock before checking authority.
func authorizeAdministrator(ctx context.Context, tx *sql.Tx, id, role string) error {
	if role == "" || id == "" {
		return user.ErrAccessDenied
	}
	result, err := tx.ExecContext(ctx, `UPDATE user SET id=id WHERE id=? AND role=? AND invited=0
 AND (verified=1 OR verification_required=0) AND registration_auth_type<>''`, id, role)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return user.ErrAccessDenied
	}
	return err
}

func (s *AccountRepository) InviteUser(ctx context.Context, actorID, administratorRole string, u *user.User, token user.TokenMetadata) error {
	if u == nil || u.ID == "" || u.Role == "" || token.AccountID != u.ID ||
		token.Purpose != user.InvitationTokenPurpose || token.TokenHash == "" ||
		user.EmailKey(token.Email) != user.EmailKey(u.Email) || !time.Now().Before(token.ExpiresAt) {
		return user.ErrToken
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := authorizeAdministrator(ctx, tx, actorID, administratorRole); err != nil {
		return err
	}
	now := time.Now()
	if !now.Before(token.ExpiresAt) {
		return user.ErrTokenExpired
	}
	if u.SessionVersion == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO user
 (id,email,email_key,role,invited,verified,verification_required,registration_auth_type,password,created_at,updated_at)
 VALUES (?,?,?,?,1,0,1,'','',?,?)`, u.ID, u.Email, user.EmailKey(u.Email), u.Role, now, now)
	} else {
		var current *user.User
		current, err = scanAccount(tx.QueryRowContext(ctx, "SELECT "+accountColumns+" FROM user WHERE id=?", u.ID))
		if errors.Is(err, sql.ErrNoRows) {
			return user.ErrUserNotFound
		}
		if err == nil && current.Role != "" && !current.Invited {
			return user.ErrAlreadyActive
		}
		if err == nil {
			var result sql.Result
			result, err = tx.ExecContext(ctx, `UPDATE user SET role=?, invited=1, updated_at=?, session_version=session_version+1
 WHERE id=? AND email_key=? AND session_version=?`, u.Role, now, u.ID, user.EmailKey(u.Email), u.SessionVersion)
			if err == nil {
				n, e := result.RowsAffected()
				err = e
				if err == nil && n != 1 {
					return user.ErrUserChanged
				}
			}
		}
	}
	if err != nil {
		return accountWriteError(err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM usertoken WHERE user_id=? AND token_type=?", u.ID, user.InvitationTokenPurpose); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO usertoken(user_id,token_hash,expires_at,token_type,email) VALUES(?,?,?,?,?)`,
		u.ID, token.TokenHash, token.ExpiresAt, token.Purpose, token.Email); err != nil {
		return err
	}
	saved, err := scanAccount(tx.QueryRowContext(ctx, "SELECT "+accountColumns+" FROM user WHERE id=?", u.ID))
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	*u = *saved
	return nil
}

func (s *AccountRepository) ChangeUserRole(ctx context.Context, actorID, administratorRole string, u *user.User) error {
	if u == nil || u.ID == "" {
		return user.ErrUserNotSpecified
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := authorizeAdministrator(ctx, tx, actorID, administratorRole); err != nil {
		return err
	}
	if u.ID == actorID && u.Role == "" {
		return user.ErrRemoveSelf
	}
	current, err := scanAccount(tx.QueryRowContext(ctx, "SELECT "+accountColumns+" FROM user WHERE id=?", u.ID))
	if errors.Is(err, sql.ErrNoRows) {
		return user.ErrUserNotFound
	}
	if err != nil {
		return err
	}
	if current.Role == "" {
		return user.ErrUserNotFound
	}
	if current.SessionVersion != u.SessionVersion {
		return user.ErrUserChanged
	}
	if current.Role == administratorRole && u.Role != administratorRole && !current.Invited && !current.NeedsVerification() {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM user WHERE role=? AND invited=0
 AND (verified=1 OR verification_required=0) AND registration_auth_type<>''`, administratorRole).Scan(&count); err != nil {
			return err
		}
		if count <= 1 {
			return user.ErrLastAdministrator
		}
	}
	if current.Role != u.Role {
		current.Role = u.Role
		if u.Role == "" {
			current.Invited = false
			if _, err := tx.ExecContext(ctx, "DELETE FROM usertoken WHERE user_id=?", u.ID); err != nil {
				return err
			}
		}
		current.SessionVersion++
		current.UpdatedAt = time.Now()
		if _, err := tx.ExecContext(ctx, "UPDATE user SET role=?,invited=?,session_version=?,updated_at=? WHERE id=?",
			current.Role, current.Invited, current.SessionVersion, current.UpdatedAt, u.ID); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	*u = *current
	return nil
}
