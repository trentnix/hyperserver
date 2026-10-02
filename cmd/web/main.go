// Command web runs HyperServer's loopback-only development application.
// It imports the sample site and email authentication provider. It is not suitable
// for public deployment.
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/middleware"
	"github.com/trentnix/hyperserver/pkg/util"
)

// main starts the application server, configures the routes, etc.
func main() {
	s := server.NewApplicationServer()
	defer func() {
		if err := s.Shutdown(); err != nil {
			log.Fatal(err)
		}
	}()

	address, err := referenceListenAddress(s.Config.HTTP.ListenHost, s.Config.HTTP.Port)
	if err != nil {
		log.Fatal(err)
	}

	// if the working directory is configured, set the working directory
	if s.Config.App.WorkingDirectory != "" {
		err := util.SetWorkingDirectory(s.Config.App.WorkingDirectory)
		if err != nil {
			log.Fatal(err)
		}
	}

	if err := setupAccountStorage(context.Background(), s); err != nil {
		log.Fatalf("failed to prepare account storage: %v", err)
	}

	// attach routes and their handlers to the router
	if err := SetupHandlers(s); err != nil {
		log.Fatalf("failed to set up the registered handlers: %v", err)
	}

	// set up authentication services and components
	if err := SetupAuthentication(s); err != nil {
		log.Fatalf("failed to set up the authorization services: %v", err)
	}

	l, err := logger.NewZapLogger()
	if err != nil {
		log.Fatalf("failed to instantiate a logger: %v", err)
	}

	mux := middleware.ChainMiddleware(s.Web,
		middleware.LoggerMiddleware(l),
		middleware.LoadSessionManagement(s.Database, s.SessionManager),
	)

	server := &http.Server{
		Addr:         address,
		Handler:      mux,
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

// referenceListenAddress keeps the development application on loopback interfaces.
// This restriction does not apply to applications built with the framework.
func referenceListenAddress(listenHost string, port uint16) (string, error) {
	host := strings.TrimSpace(listenHost)
	if host == "" || strings.EqualFold(host, "localhost") {
		host = "127.0.0.1"
	}

	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return "", fmt.Errorf("reference application must listen on a loopback address, got %q", listenHost)
	}

	return net.JoinHostPort(ip.String(), strconv.Itoa(int(port))), nil
}
