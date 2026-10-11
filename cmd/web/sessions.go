package main

import (
	"context"
	"time"

	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/session/stores"
)

// initializeSessions prepares selected providers before modules and requests use
// them. A supplied manager keeps its existing owner. Call serially during startup,
// after resolving relative storage paths. Failed initialization can be retried.
func initializeSessions(ctx context.Context, s *application) error {
	if s.SessionManager != nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	manager, err := stores.NewSessionManager(ctx, s.Config)
	if err != nil {
		return err
	}
	s.ownSessions(manager)
	return nil
}

// ownSessions transfers manager ownership before its consumers initialize.
func (s *application) ownSessions(manager *session.SessionManager) {
	s.SessionManager = manager
	s.OnShutdown("sessions", func(context.Context) error { return manager.Close() })
}
