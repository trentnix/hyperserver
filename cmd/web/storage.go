package main

import (
	"context"
	"time"

	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

const accountSetupTimeout = 10 * time.Second

// setupAccountStorage prepares this application's account tables before route setup.
// The application owns the connection pool, not the account service.
func setupAccountStorage(ctx context.Context, s *server.ApplicationServer) error {
	if !s.Config.Auth.Enabled {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, accountSetupTimeout)
	defer cancel()
	return user.PrepareDatabase(ctx, s.Database)
}
