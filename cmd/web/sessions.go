package main

import (
	"context"
	"time"

	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/session/stores"
)

// initializeSessions prepares selected providers before modules and requests use
// them. It preserves an already supplied manager. Call serially during startup,
// after resolving relative storage paths. Failed initialization can be retried.
func initializeSessions(ctx context.Context, s *server.ApplicationServer) error {
	if s.SessionManager != nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	manager, err := stores.NewSessionManager(ctx, s.Config)
	if err != nil {
		return err
	}
	s.SessionManager = manager
	return nil
}
