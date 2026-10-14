package main

import (
	"context"

	"github.com/trentnix/hyperserver/cmd/internal/setup"
)

// initializeAccounts supplies account storage before its consumers initialize.
// A supplied repository keeps its existing owner and bypasses provider selection.
func initializeAccounts(ctx context.Context, app *application, accounts *setup.AccountStorage) error {
	if app.AccountRepository != nil {
		return nil
	}
	repository, cleanup, err := accounts.Open(ctx, app.Database)
	if err != nil {
		return err
	}
	app.AccountRepository = repository
	if cleanup != nil {
		app.OnShutdown("account storage", func(context.Context) error { return cleanup() })
	}
	return nil
}
