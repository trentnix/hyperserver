// Package google implements server-side Google OpenID Connect authentication.
// Google credentials remain separate from local accounts and browser sessions.
package google

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/server"
	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"golang.org/x/oauth2"
)

const (
	// AuthType identifies this provider in configuration and routes.
	AuthType     = "google"
	issuer       = "https://accounts.google.com"
	flowSession  = "auth-google-flow"
	callbackPath = "/auth/callback/google"
)

//go:embed templates/*.html
var templates embed.FS

//go:embed static/google-g.png
var assets embed.FS

// Service implements Google login, registration, and explicit account linking.
// Dependencies belong to the application. Configure Policy before Init.
type Service struct {
	// Policy supplies application admission rules without authorizing automatic linking.
	Policy       auth.Policy
	accounts     user.AccountRepository
	identities   user.ExternalIdentityRepository
	sessions     *session.SessionManager
	content      *contentservice.ContentManagerService
	view         *content.View
	oauth        oauth2.Config
	verifier     *oidc.IDTokenVerifier
	client       *http.Client
	registration bool
}

func init() {
	auth.Register(Provider())
}

// New supplies initialized account and session services. Init performs no I/O.
func New(accounts user.AccountRepository, sessions *session.SessionManager) *Service {
	return &Service{accounts: accounts, sessions: sessions}
}

// Provider creates independent instances using application-supplied dependencies.
func Provider() auth.Descriptor {
	return auth.Descriptor{Name: AuthType, New: func(d auth.Dependencies) auth.AuthService {
		provider := New(d.Accounts, d.Sessions)
		provider.Policy = d.Policy
		return provider
	}}
}

// AuthType returns the configuration and route identifier.
func (*Service) AuthType() string { return AuthType }

// Init validates settings and prepares templates and lazy signing-key retrieval.
// HTTPS is required except for loopback development origins. Google endpoints
// are fixed, not configurable through requests or application configuration.
func (s *Service) Init(app *server.ApplicationServer) error {
	options := auth.GetAuthConfigOptions(app.Config, AuthType)
	for key := range options {
		if key != "enabled" && key != "clientID" && key != "clientSecret" {
			return fmt.Errorf("unknown auth.services.google option %q", key)
		}
	}
	if strings.TrimSpace(options["clientID"]) == "" || strings.TrimSpace(options["clientSecret"]) == "" {
		return errors.New("Google authentication requires auth.services.google.clientID and clientSecret")
	}
	origin, err := config.ParsePublicOrigin(app.Config.HTTP.PublicOrigin)
	if err != nil || origin == nil {
		return errors.New("Google authentication requires http.publicOrigin")
	}
	if origin.Scheme != "https" && origin.Hostname() != "localhost" && origin.Hostname() != "127.0.0.1" && origin.Hostname() != "::1" {
		return errors.New("Google authentication requires an HTTPS public origin outside loopback development")
	}
	identities, ok := s.accounts.(user.ExternalIdentityRepository)
	if !ok {
		return errors.New("Google authentication requires external identity account storage")
	}
	store, err := s.sessions.Store(flowSession)
	if err != nil {
		return fmt.Errorf("Google authentication requires a flow session: %w", err)
	}
	if _, ok := store.(session.RotatingStore); !ok {
		return errors.New("Google authentication requires revocable server-side flow sessions")
	}
	s.identities, s.content, s.registration = identities, app.ContentManager, app.Config.Auth.RegistrationEnabled
	s.oauth = oauth2.Config{
		ClientID: options["clientID"], ClientSecret: options["clientSecret"],
		RedirectURL: strings.TrimRight(origin.String(), "/") + callbackPath,
		Scopes:      []string{oidc.ScopeOpenID, "email"},
		Endpoint:    oauth2.Endpoint{AuthURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: "https://oauth2.googleapis.com/token", AuthStyle: oauth2.AuthStyleInParams},
	}
	s.client = &http.Client{Timeout: 10 * time.Second}
	keys := oidc.NewRemoteKeySet(oidc.ClientContext(context.Background(), s.client), "https://www.googleapis.com/oauth2/v3/certs")
	s.verifier = oidc.NewVerifier(issuer, keys, &oidc.Config{ClientID: s.oauth.ClientID, SupportedSigningAlgs: []string{oidc.RS256}})
	s.view, err = auth.PrepareProviderView(s.content, content.TemplatePath{FS: templates, Path: "templates/google.html"})
	return err
}

// ValidateVerification reports that registration verifies email through Google.
// Registration accepts only email addresses for which Google is authoritative.
func (*Service) ValidateVerification() error { return nil }

// LoginAvailable checks the stored Google link, not the registration provider.
func (s *Service) LoginAvailable(ctx context.Context, account *user.User) (bool, error) {
	return s.identities.HasExternalIdentity(ctx, account.ID, issuer)
}

// GetLoginButton returns a static, reviewed form that starts a full-page redirect.
func (*Service) GetLoginButton() template.HTML {
	return `<form method="post" action="/login" hx-boost="false" class="auth-provider-button"><input type="hidden" name="provider" value="google"><button type="submit" class="auth-google-button"><img src="/auth/google/icon.png" alt="" width="20" height="20">Sign in with Google</button></form>`
}

// GetRegisterButton starts Google registration directly from the shared form.
func (*Service) GetRegisterButton() template.HTML {
	return `<form method="post" action="/register" hx-boost="false" class="auth-provider-button"><input type="hidden" name="provider" value="google"><button type="submit" class="auth-google-button"><img src="/auth/google/icon.png" alt="" width="20" height="20">Sign up with Google</button></form>`
}
