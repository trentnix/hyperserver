// server.go defines the ApplicationServer structure and its constituents. The ApplicationServer structure
// stands as the central object used to orchestrate the hypermedia server.
package server

import (
	"fmt"
	"net/http"

	"github.com/trentnix/hyperserver/config"
)

type (
	// ApplicationServer is the application container that contains various services and
	// utilities that will be shared and used by the application
	ApplicationServer struct {
		Config *config.Config
		Web    *http.ServeMux
	}
)

// NewApplicationServer creates an instance of an ApplicationServer and does all
// necessary setup
func NewApplicationServer() *ApplicationServer {
	s := new(ApplicationServer)
	s.initConfig()
	s.initWeb()
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

// initWeb initialzes the ApplicationServer router
func (s *ApplicationServer) initWeb() {
	s.Web = http.NewServeMux()
}
