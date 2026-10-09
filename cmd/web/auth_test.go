package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/content"
)

type setupAuthService struct {
	auth.AuthService
	initErr             error
	routeErr            error
	registerRoute       bool
	initialized, routed bool
}

func (*setupAuthService) AuthType() string { return "custom" }
func (s *setupAuthService) Init(*server.ApplicationServer) error {
	s.initialized = true
	return s.initErr
}
func (s *setupAuthService) Routes(routes *routing.Routes) error {
	if s.routeErr != nil {
		return s.routeErr
	}
	if s.registerRoute {
		routes.HandleFunc("GET /custom", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	}
	s.routed = true
	return nil
}

type verifyingSetupAuthService struct {
	*setupAuthService
	validationErr error
	validated     bool
}

func (s *verifyingSetupAuthService) ValidateVerification() error {
	s.validated = true
	return s.validationErr
}

func TestAuthModuleRequiresVerificationMechanism(t *testing.T) {
	for _, tc := range []struct {
		name                                string
		registration, verification, capable bool
		initErr, validationErr, routeErr    error
		warnRatePolicy                      bool
		want                                string
	}{
		{name: "missing mechanism", registration: true, verification: true, want: "requires a verification mechanism"},
		{name: "registration disabled", verification: true},
		{name: "verification optional", registration: true},
		{name: "configured mechanism", registration: true, verification: true, capable: true},
		{name: "unavailable mechanism", registration: true, verification: true, capable: true, validationErr: errors.New("delivery unavailable"), want: "delivery unavailable"},
		{name: "initialization failed", initErr: errors.New("provider failed"), want: "provider failed"},
		{name: "route registration failed", routeErr: errors.New("invalid route policy"), want: "invalid route policy"},
		{name: "route policy warning", warnRatePolicy: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := &setupAuthService{initErr: tc.initErr, routeErr: tc.routeErr, registerRoute: tc.warnRatePolicy}
			verifier := &verifyingSetupAuthService{setupAuthService: provider, validationErr: tc.validationErr}
			catalog := []auth.Descriptor{{Name: "custom", New: func() auth.AuthService {
				if tc.capable {
					return verifier
				}
				return provider
			}}}
			cfg := &config.Config{Auth: config.AuthConfig{
				Enabled: true, RegistrationEnabled: tc.registration, RegisterRequiresVerification: tc.verification,
				JwtKey: "test-auth-signing-key", VerificationTokenExpiration: time.Hour, ResetTokenExpiration: time.Hour,
				Services: map[string]map[string]string{"custom": {"enabled": "true"}},
			}}
			cfg.HTTP.Session.Types = map[string]string{"default": "sqliteStore"}
			if tc.warnRatePolicy {
				cfg.HTTP.DefaultRateLimit = config.RateLimitConfig{Enabled: true, Requests: 200, Window: time.Minute, MaxClients: 10}
				cfg.HTTP.SharedRateLimit = config.RateLimitConfig{Enabled: true, Requests: 20, Window: time.Minute, MaxClients: 10}
			}
			log := &ratePolicyWarningLogger{}
			app := &server.ApplicationServer{Config: cfg, Web: http.NewServeMux(), ContentManager: content.NewContentManager()}
			err := SetupHandlers(context.Background(), app, log, []handlers.Descriptor{auth.Module(catalog)})
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) || provider.routed {
					t.Fatalf("error = %v, routed = %t, want %q without routes", err, provider.routed, tc.want)
				}
			} else if err != nil || !provider.routed {
				t.Fatalf("error = %v, routed = %t", err, provider.routed)
			}
			wantValidation := tc.registration && tc.verification && tc.capable && tc.initErr == nil
			if !provider.initialized || verifier.validated != wantValidation {
				t.Fatalf("initialized = %t, validated = %t", provider.initialized, verifier.validated)
			}
			if tc.warnRatePolicy {
				found := false
				for _, warning := range log.warnings {
					found = found || warning["route"] == "GET /custom"
				}
				if !found {
					t.Fatalf("missing provider route warning: %v", log.warnings)
				}
			}
		})
	}
}
