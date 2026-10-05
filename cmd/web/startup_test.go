package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/pkg/server"
)

// Use subprocesses to isolate startup exits, working directories, and registries.
func TestStartupHelperProcess(t *testing.T) {
	switch os.Getenv("HS_STARTUP_TEST_HELPER") {
	case "1":
		main()
	case "invalid-contact-storage":
		path := filepath.Join(t.TempDir(), "not-a-directory")
		if err := os.WriteFile(path, []byte("existing file"), 0600); err != nil {
			t.Fatal(err)
		}
		os.Args = append(os.Args, "-contact-directory", path)
		main()
	case "shutdown":
		checkRunShutdown(t)
	case "signals":
		runSignalTestHelper(t)
	case "listen-failure":
		s := server.NewApplicationServer()
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		s.Config.HTTP.Port = uint16(listener.Addr().(*net.TCPAddr).Port)
		if err := run(context.Background(), s); err == nil || !strings.Contains(err.Error(), "address already in use") {
			t.Fatalf("listen failure = %v", err)
		}
		if err := s.Database.Ping(); err == nil {
			t.Fatal("listen failure left the application's pool open")
		}
		fmt.Println("listen failure cleanup passed")
	case "initialize":
		// Exercise successful initialization without binding a network port.
		s := server.NewApplicationServer()
		defer s.Shutdown()
		if err := s.Database.Ping(); err != nil {
			t.Fatal(err)
		}
		if _, err := referenceListenAddress(s.Config.HTTP.ListenHost, s.Config.HTTP.Port); err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir(s.Config.App.WorkingDirectory); err != nil {
			t.Fatal(err)
		}
		if err := setupAccountStorage(context.Background(), s); err != nil {
			t.Fatal(err)
		}
		var tables int
		if err := s.Database.Get(&tables, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name IN ('user', 'usertoken')`); err != nil {
			t.Fatal(err)
		}
		if (s.Config.Auth.Enabled && tables != 2) || (!s.Config.Auth.Enabled && tables != 0) {
			t.Fatalf("account tables before handler initialization = %d, auth enabled = %t", tables, s.Config.Auth.Enabled)
		}
		if err := SetupHandlers(context.Background(), s, nil); err != nil {
			t.Fatal(err)
		}
		if err := SetupAuthentication(s, nil); err != nil {
			t.Fatal(err)
		}
		_, pattern := s.Web.Handler(httptest.NewRequest(http.MethodGet, "/auth/login", nil))
		if s.Config.Auth.Enabled && pattern != "GET /auth/login" {
			t.Fatalf("login pattern = %q, want GET /auth/login", pattern)
		}
		if !s.Config.Auth.Enabled && pattern == "GET /auth/login" {
			t.Fatal("disabled authentication registered a login route")
		}
		w := httptest.NewRecorder()
		s.Web.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Welcome!") {
			t.Fatalf("initialized application did not render its homepage: status %d, body %s", w.Code, w.Body.String())
		}
		fmt.Println("startup initialization passed")
	default:
		return
	}
}

const startupTestConfig = `http:
  listenHost: "127.0.0.1"
  port: 8080
  session:
    jwtKey: "ssssssssssssssssssssssssssssssss"
    tokenAge: 1h
    cookieAge: 1h
    stores:
      cookieStore:
        enabled: "true"
      sqliteStore:
        enabled: "true"
        connection: "file:startup-sessions?mode=memory&cache=shared"
        sessionTable: "session"
    types:
      default: cookieStore
      auth-user-session: sqliteStore
auth:
  enabled: true
  registrationEnabled: true
  jwtKey: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  verificationTokenExpiration: 24h
  resetTokenExpiration: 1h
  services:
    email:
      enabled: "true"
database:
  driver: sqlite3
  connection: ":memory:"
`

func runStartupProcess(t *testing.T, yaml, mode string, overrides ...string) ([]byte, error) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestStartupHelperProcess$")
	cmd.Dir = dir
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "HYPERSERVER_") && !strings.HasPrefix(entry, "HS_STARTUP_TEST_HELPER=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "HS_STARTUP_TEST_HELPER="+mode)
	cmd.Env = append(cmd.Env, overrides...)
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("startup subprocess timed out: %s", output)
	}
	return output, err
}

func TestStartupAcceptsValidConfiguration(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		overrides []string
	}{
		{name: "file settings"},
		{name: "environment overrides", overrides: []string{
			"HYPERSERVER_HTTP_SESSION_JWTKEY=environment-session-signing-key",
			"HYPERSERVER_AUTH_JWTKEY=environment-auth-signing-key",
			"HYPERSERVER_HTTP_SESSION_TOKENAGE=1s",
			"HYPERSERVER_HTTP_SESSION_COOKIEAGE=1s",
			"HYPERSERVER_AUTH_VERIFICATIONTOKENEXPIRATION=1s",
			"HYPERSERVER_AUTH_RESETTOKENEXPIRATION=1s",
			"HYPERSERVER_HTTP_SESSION_STORES_COOKIESTORE_ENABLED=TrUe",
			"HYPERSERVER_AUTH_SERVICES_EMAIL_ENABLED=1",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			yaml := startupTestConfig + fmt.Sprintf("app:\n  workingDirectory: %q\n", root)
			output, err := runStartupProcess(t, yaml, "initialize", tc.overrides...)
			if err != nil || !strings.Contains(string(output), "startup initialization passed") {
				t.Fatalf("valid configuration failed: %v\n%s", err, output)
			}
		})
	}
}

func TestStartupRejectsUnavailableAccountStorage(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	yaml := startupTestConfig + fmt.Sprintf("app:\n  workingDirectory: %q\n", root)
	connection := filepath.Join(t.TempDir(), "missing-directory", "accounts.db")
	output, err := runStartupProcess(t, yaml, "1", "HYPERSERVER_DATABASE_CONNECTION="+connection)
	if err == nil || !strings.Contains(string(output), "failed to prepare account storage") {
		t.Fatalf("unavailable account storage: error = %v, output = %s", err, output)
	}
	if strings.Contains(string(output), "Starting server at") {
		t.Fatalf("startup attempted to listen with unavailable account storage: %s", output)
	}
}

func TestStartupClosesPoolOnListenFailure(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	yaml := startupTestConfig + fmt.Sprintf("app:\n  workingDirectory: %q\n", root)
	output, err := runStartupProcess(t, yaml, "listen-failure")
	if err != nil || !strings.Contains(string(output), "listen failure cleanup passed") {
		t.Fatalf("listen failure cleanup: error = %v, output = %s", err, output)
	}
}

func TestStartupRejectsInvalidFileContactStorage(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	yaml := startupTestConfig + fmt.Sprintf("app:\n  workingDirectory: %q\n", root)
	output, err := runStartupProcess(t, yaml, "invalid-contact-storage")
	if err == nil || !strings.Contains(string(output), "prepare contact directory") {
		t.Fatalf("invalid contact storage: error = %v, output = %s", err, output)
	}
	if strings.Contains(string(output), "Starting server at") {
		t.Fatal("application listened after contact provider initialization failed")
	}
}

func TestStartupRejectsInvalidConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, old, replacement, env, want string
	}{
		{name: "legacy file key", old: "jwtKey: \"ssss", replacement: "key: \"ssss", want: "http.session.jwtKey"},
		{name: "missing session key", old: "ssssssssssssssssssssssssssssssss", want: "http.session.jwtKey"},
		{name: "empty environment session key", env: "HYPERSERVER_HTTP_SESSION_JWTKEY=", want: "http.session.jwtKey"},
		{name: "placeholder environment auth key", env: "HYPERSERVER_AUTH_JWTKEY=<your auth token key goes here>", want: "auth.jwtKey"},
		{name: "zero file cookie lifetime", old: "cookieAge: 1h", replacement: "cookieAge: 0s", want: "http.session.cookieAge"},
		{name: "negative environment token lifetime", env: "HYPERSERVER_HTTP_SESSION_TOKENAGE=-1s", want: "http.session.tokenAge"},
		{name: "negative environment auth lifetime", env: "HYPERSERVER_AUTH_RESETTOKENEXPIRATION=-1s", want: "auth.resetTokenExpiration"},
		{name: "unknown file store", old: "cookieStore:", replacement: "missing:", want: "unknown enabled session store"},
		{name: "unknown environment store", env: "HYPERSERVER_HTTP_SESSION_STORES_MISSING_ENABLED=true", want: "unknown enabled session store"},
		{name: "unknown environment selection", env: "HYPERSERVER_HTTP_SESSION_TYPES_DEFAULT=missing", want: "unknown or disabled store"},
		{name: "unknown file selection", old: "default: cookieStore", replacement: "default: missing", want: "http.session.types.default selects unknown or disabled store"},
		{name: "unknown named selection", env: "HYPERSERVER_HTTP_SESSION_TYPES_USER=missing", want: "http.session.types.user selects unknown or disabled store"},
		{name: "disabled selected store", env: "HYPERSERVER_HTTP_SESSION_STORES_COOKIESTORE_ENABLED=false", want: "unknown or disabled store"},
		{name: "unknown file auth provider", old: "email:", replacement: "missing:", want: "unknown enabled auth service"},
		{name: "unknown environment auth provider", env: "HYPERSERVER_AUTH_SERVICES_MISSING_ENABLED=true", want: "unknown enabled auth service"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			yaml := startupTestConfig
			if tc.old != "" {
				yaml = strings.Replace(yaml, tc.old, tc.replacement, 1)
			}
			var overrides []string
			if tc.env != "" {
				overrides = append(overrides, tc.env)
			}
			output, err := runStartupProcess(t, yaml, "1", overrides...)
			if err == nil {
				t.Fatal("invalid configuration was accepted")
			}
			if !strings.Contains(string(output), tc.want) {
				t.Fatalf("output = %s, want %q", output, tc.want)
			}
			// The constructor still uses its existing panic path for configuration errors.
			if strings.Contains(string(output), "Starting server at") {
				t.Fatalf("startup attempted to listen: %s", output)
			}
		})
	}
}

func TestStartupRegistrationVerificationDelivery(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, registration, verification string
		mail                             bool
		override                         string
		want                             string
	}{
		{name: "required delivery missing", registration: "true", verification: "true", want: "auth.registerRequiresVerification"},
		{name: "registration disabled", registration: "false", verification: "true"},
		{name: "verification optional", registration: "true", verification: "false"},
		{name: "both disabled", registration: "false", verification: "false"},
		{name: "authentication disabled", registration: "true", verification: "true", override: "HYPERSERVER_AUTH_ENABLED=false"},
		{name: "configured delivery without network", registration: "true", verification: "true", mail: true},
		{name: "missing SMTP host", registration: "true", verification: "true", mail: true, override: "HYPERSERVER_MAIL_HOSTNAME=", want: "auth.registerRequiresVerification"},
		{name: "missing SMTP port", registration: "true", verification: "true", mail: true, override: "HYPERSERVER_MAIL_PORT=0", want: "auth.registerRequiresVerification"},
		{name: "missing SMTP user", registration: "true", verification: "true", mail: true, override: "HYPERSERVER_MAIL_USER=", want: "auth.registerRequiresVerification"},
		{name: "missing SMTP password", registration: "true", verification: "true", mail: true, override: "HYPERSERVER_MAIL_PASSWORD=", want: "auth.registerRequiresVerification"},
		{name: "invalid sender", registration: "true", verification: "true", mail: true, override: "HYPERSERVER_MAIL_FROMADDRESS=not-an-address", want: "auth.registerRequiresVerification"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			yaml := startupTestConfig + fmt.Sprintf("app:\n  workingDirectory: %q\n", root)
			if tc.mail {
				yaml += "mail:\n  hostname: smtp.example.invalid\n  port: 587\n  user: test\n  password: test\n  fromAddress: test@example.invalid\n"
			}
			overrides := []string{
				"HYPERSERVER_AUTH_REGISTRATIONENABLED=" + tc.registration,
				"HYPERSERVER_AUTH_REGISTERREQUIRESVERIFICATION=" + tc.verification,
			}
			if tc.override != "" {
				overrides = append(overrides, tc.override)
			}
			output, err := runStartupProcess(t, yaml, "initialize", overrides...)
			if tc.want != "" {
				if err == nil || !strings.Contains(string(output), tc.want) {
					t.Fatalf("startup error = %v, output = %s, want %q", err, output, tc.want)
				}
			} else if err != nil || !strings.Contains(string(output), "startup initialization passed") {
				t.Fatalf("startup failed: %v\n%s", err, output)
			}
		})
	}
}
