package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/trentnix/hyperserver/auth/password"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/components/form"
	databasesqlite "github.com/trentnix/hyperserver/pkg/database/sqlite"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/session"
	sessionsqlite "github.com/trentnix/hyperserver/pkg/services/session/sqlite"
	"github.com/trentnix/hyperserver/pkg/services/user"
	usersqlite "github.com/trentnix/hyperserver/pkg/services/user/sqlite"
)

type settings struct {
	notesStore, notesPath string
	accountsPath          string
	sessionKey, authKey   string
}

func (s settings) config() (config.Config, error) {
	if s.accountsPath == "" {
		return config.Config{}, errors.New("accounts database path is required")
	}
	if err := config.ValidateSigningKey("EXAMPLE_SESSION_KEY", s.sessionKey); err != nil {
		return config.Config{}, err
	}
	if err := config.ValidateSigningKey("EXAMPLE_AUTH_KEY", s.authKey); err != nil {
		return config.Config{}, err
	}
	if s.authKey == s.sessionKey {
		return config.Config{}, errors.New("account and session signing keys must differ")
	}
	cfg := config.Config{
		App: config.AppConfig{Name: "External example"},
		Auth: config.AuthConfig{
			Enabled: true, JwtKey: s.authKey,
			VerificationTokenExpiration: time.Hour, ResetTokenExpiration: time.Hour,
			Services: map[string]map[string]string{"email": {"enabled": "true"}},
		},
	}
	return cfg, cfg.Validate()
}

// Application setup selects concrete providers and owns their cleanup. Notes
// storage is independent of the account and session database.
func openAccounts(ctx context.Context, app *server.ApplicationServer, s settings) (user.AccountRepository, *session.SessionManager, error) {
	pool, err := databasesqlite.Open(s.accountsPath)
	if err != nil {
		return nil, nil, err
	}
	app.OnShutdown("accounts", func(context.Context) error { return pool.Close() })
	accounts := usersqlite.NewAccountRepository(pool)
	if err := accounts.Initialize(ctx); err != nil {
		return nil, nil, err
	}

	sessions, err := session.NewSessionManager(ctx, map[string]string{"default": "sqlite"},
		map[string]session.StoreFactory{"sqlite": func(ctx context.Context) (session.SessionStore, error) {
			return sessionsqlite.NewSQLiteStore(ctx, sessionsqlite.Options{
				Connection: s.accountsPath, Table: "example_sessions", SigningKey: s.sessionKey,
				TokenLifetime: 24 * time.Hour, CookieLifetime: 24 * time.Hour,
			})
		}})
	if err != nil {
		return nil, nil, err
	}
	app.OnShutdown("sessions", func(context.Context) error { return sessions.Close() })
	return accounts, sessions, nil
}

// createUser is an explicit local provisioning operation, never a startup upsert.
// Repeating it must not reset an existing account's credentials.
func createUser(ctx context.Context, accounts user.AccountRepository, email, secret string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !form.ValidEmail(email) {
		return errors.New("EXAMPLE_USER_EMAIL must be a valid email address")
	}
	if err := password.ValidatePasswordComplexity(secret); err != nil {
		return fmt.Errorf("EXAMPLE_USER_PASSWORD: %w", err)
	}
	hash, err := password.HashPassword(secret)
	if err != nil {
		return err
	}
	return accounts.Create(ctx, &user.User{Email: email, Password: hash, RegistrationAuthType: "email"})
}
