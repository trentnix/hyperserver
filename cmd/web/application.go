package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/trentnix/hyperserver/cmd/internal/setup"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/database"
	"github.com/trentnix/hyperserver/pkg/database/sqlite"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
	"github.com/trentnix/hyperserver/pkg/services/messaging/smtp"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

// application owns the reference site's services outside the core runtime.
type application struct {
	*server.ApplicationServer
	// Database can be borrowed. Use ownDatabase to transfer cleanup to this application.
	Database *sql.DB
	Mail     *messaging.MailClient
	// AccountRepository must be ready before modules and middleware use it.
	// A supplied repository keeps its existing owner.
	AccountRepository user.AccountRepository
	// SessionManager can be borrowed. Use ownSessions to transfer cleanup.
	SessionManager *session.SessionManager
	mailHealth     func(context.Context) error
	contactHealth  func(context.Context) error
}

func newApplication(cfg config.Config) (*application, error) {
	core, err := server.NewApplicationServer(cfg)
	if err != nil {
		return nil, err
	}
	return &application{ApplicationServer: core}, nil
}

// ownDatabase transfers pool ownership before its consumers initialize.
func (s *application) ownDatabase(db *sql.DB) {
	s.Database = db
	s.OnShutdown("application database", func(context.Context) error { return db.Close() })
}

// initializeDatabase opens and checks the application-owned pool with a startup
// deadline. Call only when a consumer needs it, before initializing that consumer.
// A supplied pool keeps its existing owner. Failed setup leaves Database unset
// and can be retried.
// Call serially during startup, before serving requests.
func (s *application) initializeDatabase(ctx context.Context) error {
	if s.Database != nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}

	db, err := openApplicationDatabase(s.Config)
	if err != nil {
		return err
	}
	err = db.PingContext(ctx)
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return errors.Join(fmt.Errorf("connect application database: %w", err), db.Close())
	}

	s.ownDatabase(db)
	return nil
}

// openApplicationDatabase selects the web application's configured SQL pool.
// The caller must ping and close it. Supplied application pools bypass this setup.
func openApplicationDatabase(cfg *config.Config) (*sql.DB, error) {
	connection, err := database.ResolveConnection(cfg.Database.Driver, cfg.Database.Connection, cfg.App.WorkingDirectory)
	if err != nil {
		return nil, err
	}
	if cfg.Database.Driver == "sqlite3" {
		return sqlite.Open(connection)
	}
	return database.Setup(cfg.Database.Driver, connection)
}

// initializeAccounts supplies account storage before its consumers initialize.
// A supplied repository keeps its existing owner and bypasses provider selection.
func initializeAccounts(ctx context.Context, app *application, accounts *setup.AccountStorage) error {
	if app.AccountRepository != nil {
		return nil
	}
	repository, cleanup, err := accounts.Open(ctx, app.Database)
	if err != nil {
		return err
	}
	app.AccountRepository = repository
	if cleanup != nil {
		app.OnShutdown("account storage", func(context.Context) error { return cleanup() })
	}
	return nil
}

// initializeSessions prepares selected providers before modules and requests use
// them. A supplied manager keeps its existing owner. Call serially during startup,
// after resolving relative storage paths. Failed initialization can be retried.
func initializeSessions(ctx context.Context, s *application) error {
	if s.SessionManager != nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	manager, err := setup.NewSessionManager(ctx, s.Config)
	if err != nil {
		return err
	}
	s.ownSessions(manager)
	return nil
}

// ownSessions transfers manager ownership before its consumers initialize.
func (s *application) ownSessions(manager *session.SessionManager) {
	s.SessionManager = manager
	s.OnShutdown("sessions", func(context.Context) error { return manager.Close() })
}

// initializeMail supplies the reference application's SMTP client unless a client
// was already supplied. It does not contact the server. Consumers that require
// delivery must validate configuration before requests are served.
func initializeMail(s *application) error {
	if s.Mail != nil {
		return nil
	}

	client, checkConnectivity, err := smtp.NewMailClient(s.Config.Mail)
	if err != nil {
		return err
	}
	s.Mail = client
	s.mailHealth = checkConnectivity
	return nil
}
