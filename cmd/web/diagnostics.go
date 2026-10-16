package main

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"
)

// Redact configured secrets while retaining actionable startup/shutdown errors.
// Providers must not include other credentials or token values in their errors.
func redactLifecycleError(err error, app *application) string {
	message := err.Error()
	secrets := []string{app.Config.Auth.JwtKey, app.Config.HTTP.Session.JwtKey, app.Config.Mail.Password, app.Config.Database.Connection}
	for _, options := range app.Config.HTTP.Session.Stores {
		if value := options["connection"]; value != "" {
			secrets = append(secrets, value)
		}
	}
	for _, value := range app.Config.Auth.AccountStorage.Options {
		secrets = append(secrets, value)
	}
	// Longest values first so a short secret cannot expose the rest of a larger one.
	slices.SortFunc(secrets, func(a, b string) int { return len(b) - len(a) })
	for _, secret := range secrets {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[redacted]")
		}
	}
	return message
}

// logServiceProviders reports actual application selections, not provider options.
// mailProvider comes from the setup branch because supplied clients hide their sender.
func logServiceProviders(log *slog.Logger, app *application, mailProvider string) {
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
		slog.Any("accountRepository", accounts),
		slog.Any("sessionProviders", sessions),
		slog.Any("mailProvider", mailProvider))
}
