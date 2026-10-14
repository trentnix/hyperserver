package accounttest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

// memoryRepository is a test adapter, not a supported application provider.
// Values are copied at the boundary. A single lock makes each operation atomic.
// Unlike SQLite, it checks all preconditions before changing either map.
type memoryRepository struct {
	mu       sync.Mutex
	accounts map[string]user.User
	tokens   map[tokenKey]user.TokenMetadata
	now      func() time.Time
}

type tokenKey struct{ account, hash string }

func TestMemoryAccountContract(t *testing.T) {
	for _, clock := range []string{"local", "coarse remote"} {
		t.Run(clock, func(t *testing.T) {
			Run(t, func(*testing.T) user.AccountRepository {
				now := time.Now
				if clock == "coarse remote" {
					// Model a backend clock with second precision and modest skew.
					now = func() time.Time { return time.Now().Add(-2 * time.Minute).Truncate(time.Second) }
				}
				return &memoryRepository{
					accounts: make(map[string]user.User),
					tokens:   make(map[tokenKey]user.TokenMetadata),
					now:      now,
				}
			})
		})
	}
}

func (m *memoryRepository) GetByID(ctx context.Context, id string) (*user.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	u, ok := m.accounts[id]
	if !ok {
		return nil, user.NewErrUserNotFound(nil)
	}
	return &u, nil
}

func (m *memoryRepository) GetByEmail(ctx context.Context, email string) (*user.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, u := range m.accounts {
		if user.EmailKey(u.Email) == user.EmailKey(email) {
			return &u, nil
		}
	}
	return nil, user.NewErrUserNotFound(nil)
}

func (m *memoryRepository) GetToken(ctx context.Context, hash, purpose string) (user.TokenMetadata, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return user.TokenMetadata{}, err
	}
	for _, token := range m.tokens {
		if token.TokenHash == hash && token.Purpose == purpose {
			return token, nil
		}
	}
	return user.TokenMetadata{}, user.NewErrTokenNotFound(nil)
}

func (m *memoryRepository) Create(ctx context.Context, u *user.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if u == nil || u.ID != "" || u.Email == "" {
		return errors.New("invalid new account")
	}
	if m.emailExists(u.Email, "") {
		return user.ErrEmailAlreadyExists
	}

	created := *u
	created.ID = uuid.NewString()
	created.CreatedAt = m.now()
	created.UpdatedAt = created.CreatedAt
	created.SessionVersion = 1
	m.accounts[created.ID] = created
	*u = created
	return nil
}

func (m *memoryRepository) InsertToken(ctx context.Context, token user.TokenMetadata) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if token.AccountID == "" || token.TokenHash == "" || token.ExpiresAt.IsZero() ||
		(token.Purpose != user.ResetTokenPurpose && token.Purpose != user.VerificationTokenPurpose) {
		return errors.New("invalid token metadata")
	}
	if _, ok := m.accounts[token.AccountID]; !ok {
		return user.NewErrUserNotFound(nil)
	}
	key := tokenKey{token.AccountID, token.TokenHash}
	if _, ok := m.tokens[key]; ok {
		return errors.New("duplicate token")
	}
	m.tokens[key] = token
	return nil
}

func (m *memoryRepository) Update(ctx context.Context, u *user.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if u == nil || u.Email == "" {
		return errors.New("invalid account")
	}
	stored, ok := m.accounts[u.ID]
	if !ok {
		return user.NewErrUserNotFound(nil)
	}
	if stored.SessionVersion != u.SessionVersion {
		return user.NewErrUserChanged(nil)
	}
	if m.emailExists(u.Email, u.ID) {
		return user.ErrEmailAlreadyExists
	}

	updated := *u
	updated.CreatedAt = stored.CreatedAt
	updated.UpdatedAt = m.now()
	emailChanged := user.EmailKey(stored.Email) != user.EmailKey(u.Email)
	if emailChanged || stored.Password != u.Password || stored.RegistrationAuthType != u.RegistrationAuthType ||
		stored.Verified != u.Verified || stored.VerificationRequired != u.VerificationRequired {
		updated.SessionVersion++
	}
	if emailChanged {
		for key, token := range m.tokens {
			if token.AccountID == u.ID && token.Purpose == user.VerificationTokenPurpose {
				delete(m.tokens, key)
			}
		}
	}
	m.accounts[u.ID] = updated
	u.UpdatedAt, u.SessionVersion = updated.UpdatedAt, updated.SessionVersion
	return nil
}

func (m *memoryRepository) ChangePassword(ctx context.Context, u *user.User, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if u == nil || hash == "" {
		return errors.New("invalid password change")
	}
	stored, ok := m.accounts[u.ID]
	if !ok || stored.Password != u.Password || stored.RegistrationAuthType != u.RegistrationAuthType || stored.SessionVersion != u.SessionVersion {
		return errors.New("account changed or no longer exists")
	}

	stored.Password, stored.UpdatedAt = hash, m.now()
	stored.SessionVersion++
	m.accounts[u.ID] = stored
	u.Password, u.UpdatedAt, u.SessionVersion = stored.Password, stored.UpdatedAt, stored.SessionVersion
	return nil
}

func (m *memoryRepository) ResetPassword(ctx context.Context, u *user.User, auth user.ResetAuthorization, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if u == nil || hash == "" || auth.AccountID != u.ID {
		return errors.New("invalid reset")
	}
	if err := m.checkToken(auth.AccountID, auth.TokenHash, user.ResetTokenPurpose, auth.ExpiresAt); err != nil {
		return err
	}
	stored, ok := m.accounts[u.ID]
	if !ok || stored.Password != u.Password || stored.RegistrationAuthType != u.RegistrationAuthType {
		return user.NewErrToken(errors.New("account changed"))
	}

	stored.Password, stored.UpdatedAt = hash, m.now()
	stored.SessionVersion++
	m.accounts[u.ID] = stored
	delete(m.tokens, tokenKey{auth.AccountID, auth.TokenHash})
	*u = stored
	return nil
}

func (m *memoryRepository) Verify(ctx context.Context, auth user.VerificationAuthorization) (*user.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := m.checkToken(auth.AccountID, auth.TokenHash, user.VerificationTokenPurpose, auth.ExpiresAt); err != nil {
		return nil, err
	}
	stored, ok := m.accounts[auth.AccountID]
	if !ok || user.EmailKey(stored.Email) != user.EmailKey(auth.Email) {
		return nil, user.NewErrToken(errors.New("verification address changed"))
	}

	if !stored.Verified {
		stored.SessionVersion++
	}
	stored.Verified, stored.UpdatedAt = true, m.now()
	m.accounts[stored.ID] = stored
	delete(m.tokens, tokenKey{auth.AccountID, auth.TokenHash})
	return &stored, nil
}

// Helpers below run with mu held. Failed checks never change either map.
func (m *memoryRepository) emailExists(email, exceptID string) bool {
	for id, u := range m.accounts {
		if id != exceptID && user.EmailKey(u.Email) == user.EmailKey(email) {
			return true
		}
	}
	return false
}

func (m *memoryRepository) checkToken(id, hash, purpose string, signedExpiry time.Time) error {
	token, ok := m.tokens[tokenKey{id, hash}]
	if !ok || token.Purpose != purpose {
		return user.NewErrTokenNotFound(nil)
	}
	now := m.now()
	if !now.Before(token.ExpiresAt) || !now.Before(signedExpiry) {
		return user.NewErrTokenExpired(nil)
	}
	return nil
}
