package user

import (
	"context"
	"errors"
	"time"

	"github.com/trentnix/hyperserver/internal/opaquetoken"
)

// AuthToken holds a bearer token and its storage metadata.
// Persist Metadata, never Token. Email binds verification and setup to their recipient.
type AuthToken struct {
	UserId    string
	TokenHash string
	Token     string
	ExpiresAt time.Time
	Type      string
	Email     string
}

// Metadata returns storage fields without the raw bearer token.
func (token AuthToken) Metadata() TokenMetadata {
	return TokenMetadata{
		AccountID: token.UserId, TokenHash: token.TokenHash,
		Purpose: token.Type, ExpiresAt: token.ExpiresAt, Email: token.Email,
	}
}

func newAuthToken(u *User, expiration time.Duration, purpose string) (*AuthToken, error) {
	if u == nil || u.ID == "" {
		return nil, ErrUserNotSpecified
	}
	if expiration <= 0 {
		return nil, ErrTokenExpirationNotSpecified
	}
	if purpose != ResetTokenPurpose && purpose != VerificationTokenPurpose && purpose != PasswordSetupTokenPurpose {
		return nil, ErrToken
	}
	if purpose != ResetTokenPurpose && EmailKey(u.Email) == "" {
		return nil, ErrToken
	}

	raw := opaquetoken.New()
	token := &AuthToken{UserId: u.ID, Token: raw, TokenHash: opaquetoken.Hash(raw),
		ExpiresAt: time.Now().Add(expiration), Type: purpose}
	if purpose != ResetTokenPurpose {
		token.Email = u.Email
	}
	return token, nil
}

// loadToken validates format and stored metadata without consuming the token.
// Redemption must repeat the storage checks atomically with the account change.
func loadToken(ctx context.Context, accounts AccountRepository, raw, purpose string) (TokenMetadata, error) {
	if raw == "" {
		return TokenMetadata{}, ErrTokenNotSpecified
	}
	if !opaquetoken.Valid(raw) {
		return TokenMetadata{}, ErrToken
	}
	if accounts == nil {
		return TokenMetadata{}, errors.New("the account repository is not configured")
	}
	hash := opaquetoken.Hash(raw)
	stored, err := accounts.GetToken(ctx, hash, purpose)
	if err != nil {
		return TokenMetadata{}, err
	}
	if stored.AccountID == "" || stored.TokenHash != hash || stored.Purpose != purpose ||
		(purpose != ResetTokenPurpose && EmailKey(stored.Email) == "") {
		return TokenMetadata{}, ErrToken
	}
	if !time.Now().Before(stored.ExpiresAt) {
		return TokenMetadata{}, ErrTokenExpired
	}
	return stored, nil
}

func validateToken(ctx context.Context, accounts AccountRepository, raw, purpose string) (*User, error) {
	stored, err := loadToken(ctx, accounts, raw, purpose)
	if err != nil {
		return nil, err
	}
	account, err := accounts.GetByID(ctx, stored.AccountID)
	if err != nil {
		return nil, err
	}
	if account == nil || account.ID != stored.AccountID {
		return nil, errors.New("account repository returned an unexpected account")
	}
	if purpose != ResetTokenPurpose && EmailKey(account.Email) != EmailKey(stored.Email) {
		return nil, ErrToken
	}
	if !time.Now().Before(stored.ExpiresAt) {
		return nil, ErrTokenExpired
	}
	return account, nil
}
