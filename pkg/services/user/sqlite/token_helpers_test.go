package sqlite

import (
	"errors"
	"time"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

// Exercise token operations through the same public API used by applications.
func newTestToken(account *user.User, key []byte, lifetime time.Duration, purpose string) (*user.AuthToken, error) {
	switch purpose {
	case user.ResetTokenPurpose:
		token, err := user.NewAuthResetToken(account, key, lifetime)
		if err != nil {
			return nil, err
		}
		return &token.AuthToken, nil
	case user.VerificationTokenPurpose:
		token, err := user.NewAuthVerificationToken(account, key, lifetime)
		if err != nil {
			return nil, err
		}
		return &token.AuthToken, nil
	default:
		return nil, errors.New("unsupported test token purpose")
	}
}
