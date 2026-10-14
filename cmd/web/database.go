package main

import (
	"database/sql"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/database"
	"github.com/trentnix/hyperserver/pkg/database/sqlite"
)

// openApplicationDatabase selects the web application's configured SQL pool.
// The caller must ping and close it. Supplied application pools bypass this setup.
func openApplicationDatabase(cfg *config.Config) (*sql.DB, error) {
	connection, err := database.ResolveConnection(cfg.Database.Driver, cfg.Database.Connection, cfg.App.WorkingDirectory)
	if err != nil {
		return nil, err
	}
	if cfg.Database.Driver == "sqlite3" {
		return sqlite.Open(connection)
	}
	return database.Setup(cfg.Database.Driver, connection)
}
