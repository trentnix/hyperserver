package main

import (
	"fmt"
	"log/slog"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/trentnix/hyperserver/config"
)

// Redact configured secrets while retaining actionable startup/shutdown errors.
// Providers must not include other credentials or token values in their errors.
func redactLifecycleError(err error, cfg *config.Config) string {
	message := err.Error()
	secrets := []string{cfg.Auth.JwtKey, cfg.HTTP.Session.JwtKey, cfg.Mail.Password, cfg.Database.Connection}
	for _, options := range cfg.HTTP.Session.Stores {
		if value := options["connection"]; value != "" {
			secrets = append(secrets, value)
		}
	}
	for _, value := range cfg.Auth.AccountStorage.Options {
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
	if app.Accounts != nil {
		accounts = fmt.Sprintf("%T", app.Accounts)
	}
	var sessions []string
	if app.Sessions != nil {
		for _, name := range app.Sessions.Types {
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

// Set by the linker when packaging a container. Never changed at runtime.
var version = "development"
var commit = "unknown"

// buildInfo uses linker metadata for packaged builds and VCS stamps for local builds.
func buildInfo() struct {
	Version, Commit string
	Modified        bool
} {
	result := struct {
		Version, Commit string
		Modified        bool
	}{Version: version, Commit: commit}
	result.Modified = strings.HasSuffix(commit, "-dirty")
	if info, ok := debug.ReadBuildInfo(); ok {
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			result.Version = info.Main.Version
		}
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				result.Commit = setting.Value
			case "vcs.modified":
				result.Modified = setting.Value == "true"
			}
		}
	}
	return result
}
