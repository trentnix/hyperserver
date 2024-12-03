// main.go defines the entry point for the hyperserver application, creating an instance of the
// ApplicationServer, setting up the router, setting up necessary services, etc.
package main

import (
	"log"

	"github.com/trentnix/hyperserver/server"
)

// main starts the application server
func main() {
	s := server.NewApplicationServer()
	defer func() {
		if err := s.Shutdown(); err != nil {
			log.Fatal(err)
		}
	}()
}
