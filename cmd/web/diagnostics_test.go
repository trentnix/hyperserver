package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/config"
	framework "github.com/trentnix/hyperserver/pkg/application"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestLifecycleErrorRedaction(t *testing.T) {
	app := &application{Application: &framework.Application{ApplicationServer: &server.ApplicationServer{Config: &config.Config{}}}}
	app.Config.Auth.JwtKey = "account-secret"
	app.Config.HTTP.Session.JwtKey = "session-secret"
	app.Config.Mail.Password = "mail-secret"
	app.Config.Database.Connection = "database-secret"
	app.Config.HTTP.Session.Stores = map[string]map[string]string{"sqliteStore": {"connection": "storage-secret"}}
	app.Config.Auth.AccountStorage.Options = map[string]string{"password": "provider-secret"}
	message := redactLifecycleError(errors.New("startup failed: account-secret session-secret mail-secret database-secret storage-secret provider-secret"), app.Config)
	if strings.Contains(message, "-secret") || !strings.HasPrefix(message, "startup failed:") || strings.Count(message, "[redacted]") != 6 {
		t.Fatal(message)
	}
}

func TestServiceDiagnosticsUseSuppliedProvidersAndExcludeSecrets(t *testing.T) {
	app := &application{Application: &framework.Application{ApplicationServer: &server.ApplicationServer{Config: &config.Config{}}}}
	app.Config.Auth.AccountStorage.Provider = "unused-config-provider"
	app.Config.Auth.AccountStorage.Options = map[string]string{"password": "private-account-secret"}
	app.Config.HTTP.Session.Stores = map[string]map[string]string{"unused": {"jwtKey": "private-session-secret"}}
	app.Config.Mail.Password = "private-mail-secret"
	app.Accounts = &suppliedAccountRepository{}
	app.Sessions = &session.SessionManager{Types: map[string]string{"default": "Custom", "auth": "custom", "flash": "cookie"}}

	for _, mail := range []string{"smtp", "application-supplied"} {
		logs := &capturedLogs{}
		logServiceProviders(newTestLogger(logs), app, mail)
		output := logs.String()
		for _, want := range []string{"accountRepository=*main.suppliedAccountRepository", `sessionProviders="[cookie custom]"`, "mailProvider=" + mail} {
			if !strings.Contains(output, want) {
				t.Fatalf("diagnostics missing %q: %s", want, output)
			}
		}
		if strings.Contains(output, "unused") || strings.Contains(output, "private-") {
			t.Fatal("diagnostics exposed unused configuration or secrets:", output)
		}
	}
	logServiceProviders(nil, app, "smtp")
}

func TestServiceDiagnosticsWithoutOptionalServices(t *testing.T) {
	logs := &capturedLogs{}
	logServiceProviders(newTestLogger(logs), &application{Application: &framework.Application{}}, "none")
	for _, want := range []string{"accountRepository=none", "sessionProviders=[]", "mailProvider=none"} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("missing %q: %s", want, logs.String())
		}
	}
}

type suppliedAccountRepository struct{ user.AccountRepository }
