// main.go defines the entry point for the hyperserver application, creating an instance of the
// ApplicationServer, setting up the router, setting up necessary services, etc.
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/trentnix/hyperserver/server"
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

	// confirm the working directory
	wd, err := os.Getwd()
	if err != nil {
		fmt.Printf("Error getting working directory: %v\n", err)
		return
	}

	fmt.Printf("Current working directory: %s\n", wd)

	// if err := os.Chdir("../.."); err != nil {
	// 	log.Fatalf("Failed to set working directory: %v", err)
	// }

	// attach routes and their handlers to the router
	if err := SetupHandlers(s); err != nil {
		log.Fatalf("failed to build the handlers router: %v", err)
	}

	port := strconv.Itoa(int(s.Config.HTTP.Port))
	if port == "" {
		port = defaultPort
	}

	address := s.Config.HTTP.Hostname + ":" + port

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
