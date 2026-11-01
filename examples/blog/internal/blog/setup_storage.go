package blog

import (
	"context"
	"database/sql"
	"errors"
)

// setupStatus distinguishes an upgrade from a restart during onboarding.
func (s *sqliteRepository) setupStatus(ctx context.Context, email string) (complete, found bool, err error) {
	err = s.db.QueryRowContext(ctx, "SELECT complete FROM blog_setup WHERE email=?", email).Scan(&complete)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	return complete, err == nil, err
}

// initializeSetup records the initial state before the application accepts requests.
func (s *sqliteRepository) initializeSetup(ctx context.Context, email string, complete bool) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO blog_setup (email, complete) VALUES (?, ?)", email, complete)
	return err
}

// completeSetup is idempotent so concurrent authenticated requests can finish safely.
func (s *sqliteRepository) completeSetup(ctx context.Context, email string) error {
	result, err := s.db.ExecContext(ctx, "UPDATE blog_setup SET complete=1 WHERE email=?", email)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("setup record is missing")
	}
	return nil
}
