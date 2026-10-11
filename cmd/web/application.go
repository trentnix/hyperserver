package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/database"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

// application owns the reference site's services outside the core runtime.
type application struct {
	*server.ApplicationServer
	// Database can be borrowed. Use ownDatabase to transfer cleanup to this application.
	Database *sqlx.DB
	Mail     *messaging.MailClient
	// AccountRepository must be ready before modules and middleware use it.
	// A supplied repository keeps its existing owner.
	AccountRepository user.AccountRepository
}

func newApplication(cfg config.Config) (*application, error) {
	core, err := server.NewApplicationServer(cfg)
	if err != nil {
		return nil, err
	}
	return &application{ApplicationServer: core}, nil
}

// ownDatabase transfers pool ownership before its consumers initialize.
func (s *application) ownDatabase(db *sqlx.DB) {
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

	connection, err := database.ResolveConnection(s.Config.Database.Driver, s.Config.Database.Connection, s.Config.App.WorkingDirectory)
	if err != nil {
		return err
	}
	db, err := database.Setup(s.Config.Database.Driver, connection)
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
