// main.go defines the entry point for the hyperserver application, creating an instance of the
// ApplicationServer, setting up the router, setting up necessary services, etc.
package main

import (
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/trentnix/hyperserver/server"
	"github.com/trentnix/hyperserver/util"
)

const (
	defaultPort = "8080"
)

// main starts the application server, configures the routes, etc.
func main() {
	s := server.NewApplicationServer()
	defer func() {
		if err := s.Shutdown(); err != nil {
			log.Fatal(err)
		}
	}()

	// if the working directory is configured, set the working directory
	if s.Config.App.WorkingDirectory != "" {
		err := util.SetWorkingDirectory(s.Config.App.WorkingDirectory)
		if err != nil {
			log.Fatal(err)
		}
	}

	// attach routes and their handlers to the router
	if err := SetupHandlers(s); err != nil {
		log.Fatalf("failed to build the handlers router: %v", err)
	}

	port := strconv.Itoa(int(s.Config.HTTP.Port))
	if port == "" {
		port = defaultPort
	}

	address := fmt.Sprintf("%s:%s", s.Config.HTTP.Hostname, port)

	server := &http.Server{
		Addr:         address,
		Handler:      s.Web,
		ReadTimeout:  s.Config.HTTP.ReadTimeout,
		WriteTimeout: s.Config.HTTP.WriteTimeout,
		IdleTimeout:  s.Config.HTTP.IdleTimeout,
	}

	fmt.Printf("Starting server at %s\n", address)
	if err := server.ListenAndServe(); err != nil {
		if err == http.ErrServerClosed {
			fmt.Println("server closed")
		} else {
			log.Fatalf("server error: %v", err)
		}
	}
}
