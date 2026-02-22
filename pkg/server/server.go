// server.go defines the ApplicationServer structure and its constituents. The ApplicationServer structure
// stands as the central object used to orchestrate the hypermedia server.
package server

import (
	"fmt"
	"net/http"

	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/database"
	content_services "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/util"
)

type (
	// ApplicationServer is the application container that contains various services and
	// utilities that will be shared and used by the application
	ApplicationServer struct {
		Config         *config.Config
		Database       *sqlx.DB
		Web            *http.ServeMux
		ContentManager *content_services.ContentManagerService
		SessionManager *session.SessionManager
		Mail           *messaging.MailClient
	}
)

// NewApplicationServer creates an instance of an ApplicationServer and does all
// necessary setup
func NewApplicationServer() *ApplicationServer {
	s := new(ApplicationServer)

	s.initConfig()
	s.initDatabase()
	s.initWeb()
	s.initSessionManager()
	s.initContentManager()
	s.initMail()

	return s
}

// Shutdown handles any necessary shutdown so the server can shut down gracefully
func (s *ApplicationServer) Shutdown() error {
	return nil
}

// initConfig initializes the config.Config instance in the ApplicationServer
func (s *ApplicationServer) initConfig() {
	cfg, err := config.GetConfig()
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

// initSessionManager initializes the session manager with the provided configuration
func (s *ApplicationServer) initSessionManager() {
	s.SessionManager = session.NewSessionManager(s.Config)
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
