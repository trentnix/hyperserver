// Package server assembles configuration, storage, routing, and shared services.
// The current constructor initializes services unconditionally and panics on setup
// errors. It does not start an HTTP listener or initialize registered modules.
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
	}
)

// NewApplicationServer loads configuration and creates the database pool, router,
// content manager, and mail client. It panics on initialization errors. The caller
// must initialize sessions, accounts, and modules, serve HTTP, and close owned resources.
func NewApplicationServer() *ApplicationServer {
	s := new(ApplicationServer)

	s.initConfig()
	s.initDatabase()
	s.initWeb()
	s.initContentManager()
	s.initMail()

	return s
}

// Shutdown closes session providers and the application's database pool. The caller
// must first drain HTTP requests and stop other consumers. Borrowing modules must not call it.
func (s *ApplicationServer) Shutdown() error {
	var err error
	if s.SessionManager != nil {
		err = s.SessionManager.Close()
	}
	if s.Database != nil {
		err = errors.Join(err, s.Database.Close())
	}
	return err
}

// initConfig initializes the config.Config instance in the ApplicationServer
func (s *ApplicationServer) initConfig() {
	cfg, err := config.GetConfig()
	if err == nil {
		err = session.ValidateConfig(&cfg)
	}
	if err != nil {
		panic(fmt.Sprintf("there was an error loading the hyperserver configuration: %v", err))
	}

	s.Config = &cfg
}

// initDatabase initializes the database that is used and shared throughout the application
func (s *ApplicationServer) initDatabase() {
	db, err := database.Setup(s.Config.Database.Driver, s.Config.Database.Connection)
	if err != nil {
		panic(err)
	}

	s.Database = db
}

// initWeb initialzes the ApplicationServer router
func (s *ApplicationServer) initWeb() {
	s.Web = http.NewServeMux()
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

const accountSetupTimeout = 10 * time.Second

// InitializeAccounts prepares account storage when authentication is enabled.
// It preserves an already supplied repository. The default SQLite repository
// borrows the application's pool. Call serially before modules and requests use
// accounts. Failed initialization leaves AccountRepository unset and can be retried.
func (s *ApplicationServer) InitializeAccounts(ctx context.Context) error {
	if !s.Config.Auth.Enabled || s.AccountRepository != nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, accountSetupTimeout)
	defer cancel()
	repository := user.NewSQLiteAccountRepository(s.Database)
	if err := repository.Initialize(ctx); err != nil {
		return err
	}
	s.AccountRepository = repository
	return nil
}

// initContentManager loads the configuration data in the singleton ContentManager that
// is used throughout the application
func (s *ApplicationServer) initContentManager() {
	contentManager := content_services.NewContentManager()
	contentManager.HandleMessage = util.HttpMessage
	contentManager.HandleError = util.HttpError
	contentManager.HandleNotFound = util.HttpNotFound

	contentManager.Configure(s.Config)
	s.ContentManager = contentManager
}

// initMail initialize the mail client.
func (s *ApplicationServer) initMail() {
	var err error
	s.Mail, err = messaging.NewMailClient(s.Config)
	if err != nil {
		panic(fmt.Sprintf("failed to create mail client: %v", err))
	}
}
