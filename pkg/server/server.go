// server.go defines the ApplicationServer structure and its constituents. The ApplicationServer structure
// stands as the central object used to orchestrate the hypermedia server.
package server

import (
	"fmt"
	"net/http"

	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/database"
	"github.com/trentnix/hyperserver/pkg/services/session"
)

type (
	// ApplicationServer is the application container that contains various services and
	// utilities that will be shared and used by the application
	ApplicationServer struct {
		Config   *config.Config
		Database *sqlx.DB
		Web      *http.ServeMux
		Session  *session.SessionManager
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

// initSessionManager
func (s *ApplicationServer) initSessionManager() {
	s.Session = session.NewSessionManager([]byte(s.Config.HTTP.Session.Key))
}
