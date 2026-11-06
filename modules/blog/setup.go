package blog

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"sync/atomic"

	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/trentnix/hyperserver/pkg/util"
)

// setupRepository persists onboarding independently of editable site content.
type setupRepository interface {
	setupStatus(context.Context, string) (complete, found bool, err error)
	initializeSetup(context.Context, string, bool) error
	completeSetup(context.Context, string) error
}

// setupState tracks the first login separately from registration and verification.
// Only completion is cached. Pending setup always reads the current account state.
type setupState struct {
	accounts user.AccountRepository
	store    setupRepository
	email    string
	complete atomic.Bool
}

// newSetupState preserves pending setup across restarts. Existing accounts skip
// onboarding when upgrading an installation that has no setup record yet.
func newSetupState(ctx context.Context, store setupRepository, accounts user.AccountRepository, email string) (*setupState, error) {
	s := &setupState{accounts: accounts, store: store, email: email}
	complete, found, err := store.setupStatus(ctx, email)
	if err != nil {
		return nil, err
	}
	if !found {
		account, err := accounts.GetByEmail(ctx, email)
		if err != nil && !errors.Is(err, user.ErrUserNotFound) {
			return nil, err
		}
		complete = account != nil && !account.NeedsVerification()
		if err := store.initializeSetup(ctx, email, complete); err != nil {
			return nil, err
		}
	}
	s.complete.Store(complete)
	return s, nil
}

// finish records completion only after a request has an authenticated administrator.
func (s *setupState) finish(ctx context.Context) error {
	if err := s.store.completeSetup(ctx, s.email); err != nil {
		return err
	}
	s.complete.Store(true)
	return nil
}

// requireSetup keeps content private until registration and required verification
// finish, then guides the first authenticated administrator visit to Site Profile.
func (m *module) requireSetup(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s := m.access.setup
		if s.complete.Load() {
			next(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		account, err := s.accounts.GetByEmail(r.Context(), s.email)
		if errors.Is(err, user.ErrUserNotFound) {
			if !m.app.Config.Auth.RegistrationEnabled {
				m.setupError(w, r, "Setup requires an administrator account. Enable auth.registrationEnabled in the application configuration to register.", nil)
				return
			}
			util.RedirectToURL(w, r, "/register")
			return
		}
		if err != nil {
			m.setupError(w, r, "Setup is temporarily unavailable. Please try again.", err)
			return
		}
		if account.NeedsVerification() {
			target := loginURL
			if user.GetUserFromContext(r.Context()) != nil {
				target = "/auth/request/verify"
			}
			util.RedirectToURL(w, r, target)
			return
		}
		permission, err := m.access.Role(r.Context(), user.GetUserFromContext(r.Context()))
		if err != nil {
			m.setupError(w, r, "Unable to check your site access. Please try again.", err)
			return
		}
		if permission == roleAdmin {
			if err := s.finish(r.Context()); err != nil {
				m.setupError(w, r, "Setup could not be completed. Please try again.", err)
				return
			}
			util.RedirectToURL(w, r, "/profile")
			return
		}
		next(w, r)
	}
}

// continueSetup receives authentication redirects without closing the public home
// page. requireSetup handles verification and the first authenticated visit.
func (m *module) continueSetup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	target := "/"
	if !m.access.setup.complete.Load() {
		target = loginURL
	}
	util.RedirectToURL(w, r, target)
}

// setupPageKey keeps failed setup responses out of the public site's navigation.
type setupPageKey struct{}

// setupError reports a recoverable failure without exposing stored site content.
func (m *module) setupError(w http.ResponseWriter, r *http.Request, message string, err error) {
	r = r.WithContext(context.WithValue(r.Context(), setupPageKey{}, true))
	m.app.ContentManager.HandleError(w, r, message, err, http.StatusServiceUnavailable)
}

func (s *sqliteRepository) initialSetupEmail(ctx context.Context, fallback string) (string, error) {
	var email string
	err := s.db.QueryRowContext(ctx, "SELECT email FROM blog_setup ORDER BY rowid LIMIT 1").Scan(&email)
	if errors.Is(err, sql.ErrNoRows) {
		return fallback, nil
	}
	return email, err
}
