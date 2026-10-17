package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/trentnix/hyperserver/auth/password"
	authweb "github.com/trentnix/hyperserver/auth/web"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/modules/blog"
	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/middleware"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

type settings struct {
	dataDir, publicOrigin, title, description string
	adminEmail, authKey, sessionKey           string
}

func (s settings) config() (config.Config, error) {
	if strings.TrimSpace(s.dataDir) == "" || strings.TrimSpace(s.title) == "" {
		return config.Config{}, errors.New("blog data directory and title are required")
	}
	if !form.ValidEmail(s.adminEmail) {
		return config.Config{}, errors.New("BLOG_ADMIN_EMAIL must be a valid email address")
	}
	if err := config.ValidateSigningKey("BLOG_AUTH_KEY", s.authKey); err != nil {
		return config.Config{}, err
	}
	if err := config.ValidateSigningKey("BLOG_SESSION_KEY", s.sessionKey); err != nil {
		return config.Config{}, err
	}
	if s.authKey == s.sessionKey {
		return config.Config{}, errors.New("account and session signing keys must differ")
	}
	cfg := config.Config{
		App:  config.AppConfig{Name: s.title},
		HTTP: config.HTTPConfig{PublicOrigin: s.publicOrigin},
		Auth: config.AuthConfig{
			Enabled: true, JwtKey: s.authKey,
			ResetTokenExpiration: time.Hour, VerificationTokenExpiration: time.Hour,
			Services: map[string]map[string]string{"email": {"enabled": "true"}},
		},
	}
	return cfg, cfg.Validate()
}

type application struct {
	*server.ApplicationServer
	accounts user.AccountRepository
	handler  http.Handler
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
	stores, err := openStorage(ctx, core, s)
	if err != nil {
		return nil, errors.Join(err, app.Shutdown())
	}
	app.accounts = stores.accounts
	if err := blog.ConfigureRendering(app.ContentManager); err != nil {
		return nil, errors.Join(err, app.Shutdown())
	}
	app.ContentManager.BuildLayoutData = func(r *http.Request) any {
		data := blog.LayoutData{Title: s.title, Description: s.description}
		if u := user.GetUserFromContext(r.Context()); u != nil {
			data.SignedIn = true
			data.Email = u.Email
			data.Admin = !u.NeedsVerification() && user.EmailKey(u.Email) == user.EmailKey(s.adminEmail)
		}
		return data
	}
	accountWeb := authweb.Email{
		Accounts: app.accounts, Sessions: stores.sessions,
		LoginURL: "/auth/login/email", HomeURL: "/",
	}
	catalog, err := handlers.Replace(handlers.Registered(), blog.Module(blog.Options{
		Posts: stores.posts, Images: stores.images,
		Protect:     accountWeb.RequireAuthentication(app.ContentManager),
		AdminEmails: []string{s.adminEmail}, Title: s.title,
		Description: s.description, PublicOrigin: s.publicOrigin,
	}))
	if err != nil {
		return nil, errors.Join(err, app.Shutdown())
	}
	app.handler, err = accountWeb.Setup(ctx, core, catalog)
	if err != nil {
		return nil, errors.Join(err, app.Shutdown())
	}
	return app, nil
}

func newHTTPServer(app *application) *http.Server {
	csp := "default-src 'none'; script-src 'self'; style-src 'self'; font-src 'self'; img-src 'self' https:; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"
	return &http.Server{
		Handler:           middleware.SecurityHeaders(csp)(http.NewCrossOriginProtection().Handler(app.handler)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       time.Minute,
	}
}

// createAdmin provisions a new account explicitly. It never replaces credentials.
func createAdmin(ctx context.Context, accounts user.AccountRepository, email, secret string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !form.ValidEmail(email) {
		return errors.New("BLOG_ADMIN_EMAIL must be a valid email address")
	}
	if err := password.ValidatePasswordComplexity(secret); err != nil {
		return err
	}
	hash, err := password.HashPassword(secret)
	if err != nil {
		return err
	}
	return accounts.Create(ctx, &user.User{Email: email, Password: hash, RegistrationAuthType: "email"})
}
