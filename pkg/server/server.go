// Package server assembles routing, rendering, and application-owned services.
// Applications explicitly initialize the services their modules require before
// accepting requests and close them after requests drain.
package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/database"
	content_services "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/trentnix/hyperserver/pkg/util"
)

type (
	// ApplicationServer is the application container that contains various services and
	// utilities that will be shared and used by the application
	ApplicationServer struct {
		Config *config.Config

		// Database is owned by the application. Modules borrow it and must not close it.
		Database *sqlx.DB
		// AccountRepository supplies authentication's account and token operations.
		// An application-supplied repository must be ready to use. Its supplier owns
		// any resources not already owned by the application.
		AccountRepository user.AccountRepository
		Web               *http.ServeMux
		ContentManager    *content_services.ContentManagerService
		SessionManager    *session.SessionManager
		Mail              *messaging.MailClient
		accountProviders  map[string]AccountProvider
		closeAccounts     func() error
		shutdownHooks     []shutdownHook
	}
)

// NewApplicationServer validates shared settings and creates routing and rendering
// services. It does not load files, open storage, or initialize optional services.
// The caller initializes required services and modules before serving HTTP, then
// calls Shutdown after requests drain. The server owns a configuration copy.
// Configure that copy before initializing services and serving requests.
func NewApplicationServer(cfg config.Config) (*ApplicationServer, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("application configuration: %w", err)
	}
	cfg = cfg.Clone()
	s := &ApplicationServer{Config: &cfg, Web: http.NewServeMux()}
	s.initContentManager()
	return s, nil
}

// Shutdown runs registered cleanup, then closes account providers, session providers,
// and the application database with a ten-second cooperative deadline. Call serially
// after draining requests and stopping other consumers. Borrowing modules must not call it.
func (s *ApplicationServer) Shutdown() error {
	return s.ShutdownContext(context.Background())
}

// OnShutdown registers application-owned cleanup during startup. Hooks run once,
// in reverse registration order, before account, session, and database services
// close. Call serially, registering providers before consumers. Hooks must honor ctx.
// A nil function is ignored.
func (s *ApplicationServer) OnShutdown(name string, close func(context.Context) error) {
	if close == nil {
		return
	}
	s.shutdownHooks = append(s.shutdownHooks, shutdownHook{name: name, close: close})
}

type shutdownHook struct {
	name  string
	close func(context.Context) error
}

// ShutdownContext runs cleanup after requests and other consumers have stopped.
// It uses ctx's deadline, or ten seconds if none is set. Cleanup is cooperative:
// context-free provider close functions cannot be interrupted. All hooks are
// attempted even after an error or cancellation. Call serially, never during requests.
func (s *ApplicationServer) ShutdownContext(ctx context.Context) error {
	if _, bounded := ctx.Deadline(); !bounded {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
	}
	var err error
	hooks := s.shutdownHooks
	s.shutdownHooks = nil
	for i := len(hooks) - 1; i >= 0; i-- {
		if closeErr := errors.Join(hooks[i].close(ctx), ctx.Err()); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("shutdown %q: %w", hooks[i].name, closeErr))
		}
	}
	if s.closeAccounts != nil {
		closeAccounts := s.closeAccounts
		s.closeAccounts = nil
		err = errors.Join(err, closeAccounts())
	}
	if s.SessionManager != nil {
		err = errors.Join(err, s.SessionManager.Close())
	}
	if s.Database != nil {
		err = errors.Join(err, s.Database.Close())
	}
	return errors.Join(err, ctx.Err())
}

// InitializeDatabase opens and checks the application-owned pool with a startup
// deadline. Call only when a consumer needs it, before initializing that consumer.
// A supplied pool is preserved. Failed setup leaves Database unset and can be retried.
// Call serially during startup, before serving requests.
func (s *ApplicationServer) InitializeDatabase(ctx context.Context) error {
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
	s.Database = db
	return nil
}

// InitializeSessions prepares selected providers before modules and requests use
// them. It preserves an already supplied manager. Call serially during startup,
// after resolving relative storage paths. Failed initialization can be retried.
func (s *ApplicationServer) InitializeSessions(ctx context.Context) error {
	if s.SessionManager != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	manager, err := session.NewSessionManager(ctx, s.Config)
	if err != nil {
		return err
	}
	s.SessionManager = manager
	return nil
}

// initContentManager prepares this application's rendering defaults and callbacks.
func (s *ApplicationServer) initContentManager() {
	contentManager := content_services.NewContentManager()
	contentManager.HandleMessage = util.HttpMessage
	contentManager.HandleError = util.HttpError
	contentManager.HandleNotFound = util.HttpNotFound

	contentManager.Configure(s.Config)
	s.ContentManager = contentManager
}

// InitializeMail creates the mail client when an application needs mail operations.
// It preserves a supplied client and does not contact the delivery service.
// Construction does not guarantee configured delivery. Consumers that require
// delivery must also call Mail.ValidateConfig at startup. Call serially before requests.
func (s *ApplicationServer) InitializeMail() error {
	if s.Mail != nil {
		return nil
	}
	client, err := messaging.NewMailClient(s.Config)
	if err != nil {
		return err
	}
	s.Mail = client
	return nil
}
