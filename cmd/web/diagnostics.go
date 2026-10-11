package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/trentnix/hyperserver/pkg/services/logger"
)

// logServiceProviders reports actual application selections, not provider options.
// mailProvider comes from the setup branch because supplied clients hide their sender.
func logServiceProviders(log logger.Logger, app *application, mailProvider string) {
	if log == nil {
		return
	}
	accounts := "none"
	if app.AccountRepository != nil {
		accounts = fmt.Sprintf("%T", app.AccountRepository)
	}
	var sessions []string
	if app.SessionManager != nil {
		for _, name := range app.SessionManager.Types {
			sessions = append(sessions, strings.ToLower(name))
		}
		slices.Sort(sessions)
		sessions = slices.Compact(sessions)
	}
	log.Info("Application service providers selected",
		logger.Field{Key: "accountRepository", Value: accounts},
		logger.Field{Key: "sessionProviders", Value: sessions},
		logger.Field{Key: "mailProvider", Value: mailProvider})
}
