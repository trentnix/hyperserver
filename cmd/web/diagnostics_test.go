package main

import (
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/session"
)

func TestServiceDiagnosticsUseSuppliedProvidersAndExcludeSecrets(t *testing.T) {
	app, err := newApplication(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	app.Config.Auth.AccountStorage.Provider = "unused-config-provider"
	app.Config.Auth.AccountStorage.Options = map[string]string{"password": "private-account-secret"}
	app.Config.HTTP.Session.Stores = map[string]map[string]string{"unused": {"jwtKey": "private-session-secret"}}
	app.Config.Mail.Password = "private-mail-secret"
	app.AccountRepository = &suppliedAccountRepository{}
	app.SessionManager = &session.SessionManager{Types: map[string]string{"default": "Custom", "auth": "custom", "flash": "cookie"}}

	for _, mail := range []string{"smtp", "application-supplied"} {
		logs := &capturedLogs{}
		logServiceProviders(&testLogger{logs: logs}, app, mail)
		output := logs.String()
		for _, want := range []string{"accountRepository *main.suppliedAccountRepository", "sessionProviders [cookie custom]", "mailProvider " + mail} {
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
	logServiceProviders(&testLogger{logs: logs}, &application{}, "none")
	for _, want := range []string{"accountRepository none", "sessionProviders []", "mailProvider none"} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("missing %q: %s", want, logs.String())
		}
	}
}
