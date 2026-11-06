package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/mattn/go-sqlite3"
	"github.com/trentnix/hyperserver/pkg/database"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

var _ user.ExternalIdentityRepository = (*AccountRepository)(nil)
var _ user.ExternalIdentityUnlinker = (*AccountRepository)(nil)

// HasExternalIdentity reports whether the account has a login at the issuer.
func (s *AccountRepository) HasExternalIdentity(ctx context.Context, accountID, issuer string) (bool, error) {
	if s.db == nil {
		return false, database.ErrDatabaseUnavailable
	}
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM external_identity WHERE user_id = ? AND issuer = ?)`, accountID, issuer).Scan(&exists)
	return exists, err
}

// GetExternalAccount loads the local account linked to an issuer and subject.
func (s *AccountRepository) GetExternalAccount(ctx context.Context, identity user.ExternalIdentity) (*user.User, error) {
	if s.db == nil {
		return nil, database.ErrDatabaseUnavailable
	}
	account, err := scanAccount(s.db.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM user
		WHERE id = (SELECT user_id FROM external_identity WHERE issuer = ? AND subject = ?)`, identity.Issuer, identity.Subject))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, user.ErrUserNotFound
	}
	return account, err
}

// CreateExternalAccount creates an account and its login identity in one transaction.
func (s *AccountRepository) CreateExternalAccount(ctx context.Context, account *user.User, identity user.ExternalIdentity) error {
	if s.db == nil {
		return database.ErrDatabaseUnavailable
	}
	if account == nil || account.ID != "" || user.EmailKey(account.Email) == "" || identity.Issuer == "" || identity.Subject == "" {
		return errors.New("new external account requires email, issuer, and subject")
	}
	created := *account
	created.ID, created.SessionVersion = uuid.NewString(), 1
	created.CreatedAt = time.Now()
	created.UpdatedAt = created.CreatedAt
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO user
		(id, email, email_key, verified, verification_required, created_at, updated_at, registration_auth_type, password, session_version, role, invited)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, created.ID, created.Email, user.EmailKey(created.Email), created.Verified,
		created.VerificationRequired, created.CreatedAt, created.UpdatedAt, created.RegistrationAuthType, created.Password, created.SessionVersion, created.Role, created.Invited)
	if err != nil {
		return accountWriteError(err)
	}
	if err := insertExternalIdentity(ctx, tx, created.ID, identity); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	*account = created
	return nil
}

// LinkExternalIdentity binds a verified account without replacing existing links.
func (s *AccountRepository) LinkExternalIdentity(ctx context.Context, account *user.User, identity user.ExternalIdentity) error {
	if s.db == nil {
		return database.ErrDatabaseUnavailable
	}
	if account == nil || account.ID == "" || identity.Issuer == "" || identity.Subject == "" || account.NeedsVerification() {
		return errors.New("linking requires a verified account and external identity")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	updated := time.Now()
	result, err := tx.ExecContext(ctx, `UPDATE user SET session_version = session_version + 1, updated_at = ?
		WHERE id = ? AND session_version = ? AND (verified = 1 OR verification_required = 0)`, updated, account.ID, account.SessionVersion)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return user.ErrUserChanged
	}
	if err := insertExternalIdentity(ctx, tx, account.ID, identity); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	account.SessionVersion++
	account.UpdatedAt = updated
	return nil
}

func insertExternalIdentity(ctx context.Context, tx *sql.Tx, accountID string, identity user.ExternalIdentity) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO external_identity (issuer, subject, user_id) VALUES (?, ?, ?)`, identity.Issuer, identity.Subject, accountID)
	var sqliteErr sqlite3.Error
	if errors.As(err, &sqliteErr) && (sqliteErr.ExtendedCode == sqlite3.ErrConstraintUnique || sqliteErr.ExtendedCode == sqlite3.ErrConstraintPrimaryKey) {
		return fmt.Errorf("%w: %w", user.ErrIdentityConflict, err)
	}
	return err
}

// UnlinkExternalIdentity applies an authorized removal to the same account version.
func (s *AccountRepository) UnlinkExternalIdentity(ctx context.Context, account *user.User, issuer string) error {
	if s.db == nil {
		return database.ErrDatabaseUnavailable
	}
	if account == nil || account.ID == "" || account.NeedsVerification() || issuer == "" {
		return errors.New("disconnecting requires a verified account and issuer")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	updated := time.Now()
	result, err := tx.ExecContext(ctx, `UPDATE user SET session_version = session_version + 1, updated_at = ?
		WHERE id = ? AND session_version = ? AND (verified = 1 OR verification_required = 0)`, updated, account.ID, account.SessionVersion)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return user.ErrUserChanged
	}
	result, err = tx.ExecContext(ctx, `DELETE FROM external_identity WHERE user_id = ? AND issuer = ?`, account.ID, issuer)
	if err != nil {
		return err
	}
	n, err = result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return user.ErrUserChanged
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	account.SessionVersion++
	account.UpdatedAt = updated
	return nil
}
