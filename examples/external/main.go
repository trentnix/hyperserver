// Command greeting demonstrates consuming HyperServer from another Go module.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"example.com/greeting/greeting"
	"example.com/greeting/notes"
	"github.com/trentnix/hyperserver/auth"
	authmiddleware "github.com/trentnix/hyperserver/auth/middleware"
	emailauth "github.com/trentnix/hyperserver/auth/modules/email"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/flash"
	"github.com/trentnix/hyperserver/pkg/services/middleware"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

type application struct {
	*server.ApplicationServer
	accounts user.AccountRepository
	sessions *session.SessionManager
}

type layoutData struct {
	SignedIn, CanWrite, NeedsVerification, RegistrationEnabled bool
}

func main() {
	port := flag.Int("port", 8083, "loopback HTTP port (0 chooses an available port)")
	provider := flag.String("notes-store", "files", "notes storage provider: files or sqlite")
	path := flag.String("notes-path", "", "notes storage location (defaults depend on provider)")
	accountsPath := flag.String("accounts-path", "accounts.db", "account and session SQLite database")
	provision := flag.Bool("create-user", false, "create an account from EXAMPLE_USER_EMAIL and EXAMPLE_USER_PASSWORD, then exit")
	registration := flag.Bool("registration", false, "enable registration with required email verification")
	publicOrigin := flag.String("public-origin", "", "origin for email links (defaults to the loopback HTTP address)")
	smtpPort := flag.Int("smtp-port", 11025, "local Mailpit SMTP port (0 disables delivery)")
	flag.Parse()
	if *port < 0 || *port > 65535 {
		log.Fatal("port must be between 0 and 65535")
	}
	if *smtpPort < 0 || *smtpPort > 65535 {
		log.Fatal("smtp-port must be between 0 and 65535")
	}
	if *publicOrigin == "" && *port != 0 {
		*publicOrigin = fmt.Sprintf("http://127.0.0.1:%d", *port)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	app, err := newApplication(ctx, settings{
		notesStore: *provider, notesPath: *path, accountsPath: *accountsPath,
		sessionKey: os.Getenv("EXAMPLE_SESSION_KEY"), authKey: os.Getenv("EXAMPLE_AUTH_KEY"),
		registration: *registration, publicOrigin: *publicOrigin, smtpPort: uint16(*smtpPort),
	})
	if err != nil {
		log.Fatal(err)
	}
	if *provision {
		err := createUser(ctx, app.accounts, os.Getenv("EXAMPLE_USER_EMAIL"), os.Getenv("EXAMPLE_USER_PASSWORD"))
		if err := errors.Join(err, app.Shutdown()); err != nil {
			log.Fatal(err)
		}
		log.Print("Account created")
		return
	}
	if err := app.Run(ctx, func(context.Context) (*http.Server, net.Listener, error) {
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
		if err != nil {
			return nil, nil, err
		}
		log.Printf("Example application listening at http://%s", listener.Addr())
		return newHTTPServer(app), listener, nil
	}); err != nil {
		log.Fatal(err)
	}
}

func newApplication(ctx context.Context, s settings) (*application, error) {
	cfg, err := s.config()
	if err != nil {
		return nil, err
	}
	core, err := server.NewApplicationServer(cfg)
	if err != nil {
		return nil, err
	}
	app := &application{ApplicationServer: core}
	app.accounts, app.sessions, err = openAccounts(ctx, core, s)
	if err != nil {
		return nil, errors.Join(err, app.Shutdown())
	}
	repository, closeStore, err := openNotes(ctx, s.notesStore, s.notesPath)
	if err != nil {
		return nil, errors.Join(err, app.Shutdown())
	}
	if closeStore != nil {
		app.OnShutdown("notes storage", func(context.Context) error { return closeStore() })
	}
	if err := greeting.ConfigureRendering(app.ContentManager); err != nil {
		return nil, errors.Join(err, app.Shutdown())
	}
	app.ContentManager.AuthURL = "/auth/login/email"
	app.ContentManager.HomeURL = "/notes"
	app.ContentManager.LoadNotifications = flash.LoadNotifications
	app.ContentManager.BuildLayoutData = func(r *http.Request) any {
		data := layoutData{RegistrationEnabled: s.registration}
		if u := user.GetUserFromContext(r.Context()); u != nil {
			data.SignedIn = true
			data.NeedsVerification = u.NeedsVerification()
			data.CanWrite = !data.NeedsVerification
		}
		return data
	}
	protect := authmiddleware.RequireAuthentication(app.accounts, app.ContentManager)
	catalog, err := handlers.Replace(handlers.Registered(), notes.Module(repository, protect))
	if err != nil {
		return nil, errors.Join(err, app.Shutdown())
	}
	mail, err := configureMail(s)
	if err != nil {
		return nil, errors.Join(err, app.Shutdown())
	}
	providers := []auth.Descriptor{emailauth.Provider(app.accounts, app.sessions, mail)}
	catalog, err = handlers.Replace(catalog, auth.Module(app.accounts, app.sessions, providers))
	if err != nil {
		return nil, errors.Join(err, app.Shutdown())
	}
	if err := handlers.Setup(ctx, core, nil, catalog, nil); err != nil {
		return nil, errors.Join(err, app.Shutdown())
	}
	return app, nil
}

func newHTTPServer(app *application) *http.Server {
	handler := authmiddleware.LoadSessionManagement(app.accounts, app.sessions)(app.Web)
	return &http.Server{
		Handler:           middleware.SecurityHeaders("default-src 'none'; style-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")(http.NewCrossOriginProtection().Handler(handler)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       time.Minute,
	}
}
