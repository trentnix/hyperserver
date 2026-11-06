package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

func (s *AccountRepository) RegisterInvited(ctx context.Context, u *user.User, token *user.TokenMetadata, identity *user.ExternalIdentity) error {
	if u == nil || u.Email == "" || u.RegistrationAuthType == "" || (identity == nil && u.Password == "") {
		return user.ErrUserNotSpecified
	}
	if identity != nil && (!u.Verified || identity.Issuer == "" || identity.Subject == "") {
		return user.ErrToken
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Lock before loading the placeholder. Concurrent registrations cannot replace
	// one another's credentials, and canceled invitations cannot be resurrected.
	if _, err := tx.ExecContext(ctx, "UPDATE user SET id=id WHERE email_key=?", user.EmailKey(u.Email)); err != nil {
		return err
	}
	current, err := scanAccount(tx.QueryRowContext(ctx, "SELECT "+accountColumns+" FROM user WHERE email_key=?", user.EmailKey(u.Email)))
	if errors.Is(err, sql.ErrNoRows) {
		return user.ErrUserNotFound
	}
	if err != nil {
		return err
	}
	if !current.Invited || current.Role == "" || current.RegistrationAuthType != "" || current.Password != "" {
		return user.ErrEmailAlreadyExists
	}
	verified := identity != nil
	if token != nil {
		if token.Purpose != user.InvitationTokenPurpose || token.AccountID != current.ID || user.EmailKey(token.Email) != user.EmailKey(current.Email) {
			return user.ErrToken
		}
		if _, err := deleteTokenAndCheckExpiry(ctx, tx, current.ID, token.TokenHash, token.Purpose, current.Email, token.ExpiresAt); err != nil {
			return err
		}
		verified = true
	}
	current.Password, current.RegistrationAuthType = u.Password, u.RegistrationAuthType
	current.Verified, current.VerificationRequired = verified, u.VerificationRequired
	current.Invited = !verified && u.VerificationRequired
	current.UpdatedAt, current.SessionVersion = time.Now(), current.SessionVersion+1
	if _, err := tx.ExecContext(ctx, `UPDATE user SET password=?,registration_auth_type=?,verified=?,verification_required=?,
 invited=?,updated_at=?,session_version=? WHERE id=?`, current.Password, current.RegistrationAuthType, current.Verified,
		current.VerificationRequired, current.Invited, current.UpdatedAt, current.SessionVersion, current.ID); err != nil {
		return err
	}
	if identity != nil {
		if err := insertExternalIdentity(ctx, tx, current.ID, *identity); err != nil {
			return err
		}
	}
	if !current.Invited {
		if _, err := tx.ExecContext(ctx, "DELETE FROM usertoken WHERE user_id=? AND token_type IN (?,?)",
			current.ID, user.InvitationTokenPurpose, user.VerificationTokenPurpose); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	*u = *current
	return nil
}

func (s *AccountRepository) AcceptInvitation(ctx context.Context, u *user.User, token *user.TokenMetadata) error {
	if u == nil || u.ID == "" || u.RegistrationAuthType == "" {
		return user.ErrUserNotSpecified
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if token != nil {
		if token.Purpose != user.InvitationTokenPurpose || token.AccountID != u.ID || user.EmailKey(token.Email) != user.EmailKey(u.Email) {
			return user.ErrToken
		}
		if _, err := deleteTokenAndCheckExpiry(ctx, tx, u.ID, token.TokenHash, token.Purpose, u.Email, token.ExpiresAt); err != nil {
			return err
		}
	}
	saved, err := scanAccount(tx.QueryRowContext(ctx, `UPDATE user SET invited=0,verified=(verified=1 OR ?),session_version=session_version+1,updated_at=?
 WHERE id=? AND email_key=? AND session_version=? AND invited=1 AND role<>''
 AND registration_auth_type<>'' AND (verified=1 OR verification_required=0 OR ?)
 RETURNING `+accountColumns, token != nil, time.Now(), u.ID, user.EmailKey(u.Email), u.SessionVersion, token != nil))
	if errors.Is(err, sql.ErrNoRows) {
		return user.ErrUserChanged
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM usertoken WHERE user_id=? AND token_type IN (?,?)",
		u.ID, user.InvitationTokenPurpose, user.VerificationTokenPurpose); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	*u = *saved
	return nil
}
