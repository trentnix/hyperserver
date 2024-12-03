package main

import (
	"log"

	"github.com/trentnix/hyperserver/server"
)

func main() {
	s := server.NewApplicationServer()
	defer func() {
		if err := s.Shutdown(); err != nil {
			log.Fatal(err)
		}
	}()
}
