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

	"github.com/trentnix/hyperserver/config"
	content_services "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/trentnix/hyperserver/pkg/util"
)

type (
	// ApplicationServer is the application container that contains various services and
	// utilities that will be shared and used by the application
	ApplicationServer struct {
		Config *config.Config

		// AccountRepository supplies authentication's account and token operations.
		// An application-supplied repository must be ready to use. Its supplier owns
		// any resources not already owned by the application.
		AccountRepository user.AccountRepository
		Web               *http.ServeMux
		ContentManager    *content_services.ContentManagerService
		SessionManager    *session.SessionManager
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

// Shutdown runs registered cleanup, then closes session providers
// with a ten-second cooperative deadline. Call serially
// after draining requests and stopping other consumers. Borrowing modules must not call it.
func (s *ApplicationServer) Shutdown() error {
	return s.ShutdownContext(context.Background())
}

// OnShutdown registers application-owned cleanup during startup. Hooks run once,
// in reverse registration order, before session services
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

	if s.SessionManager != nil {
		err = errors.Join(err, s.SessionManager.Close())
	}
	return errors.Join(err, ctx.Err())
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
