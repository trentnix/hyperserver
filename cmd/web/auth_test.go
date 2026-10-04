package main

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/server"
)

type setupAuthService struct {
	auth.AuthService
	initErr             error
	initialized, routed bool
}

func (*setupAuthService) AuthType() string { return "custom" }
func (s *setupAuthService) Init(*server.ApplicationServer) error {
	s.initialized = true
	return s.initErr
}
func (s *setupAuthService) Routes(*http.ServeMux) { s.routed = true }

type verifyingSetupAuthService struct {
	*setupAuthService
	validationErr error
	validated     bool
}

func (s *verifyingSetupAuthService) ValidateVerification() error {
	s.validated = true
	return s.validationErr
}

func TestSetupAuthenticationRequiresVerificationMechanism(t *testing.T) {
	for _, tc := range []struct {
		name                                string
		registration, verification, capable bool
		initErr, validationErr              error
		want                                string
	}{
		{name: "missing mechanism", registration: true, verification: true, want: "requires a verification mechanism"},
		{name: "registration disabled", verification: true},
		{name: "verification optional", registration: true},
		{name: "configured mechanism", registration: true, verification: true, capable: true},
		{name: "unavailable mechanism", registration: true, verification: true, capable: true, validationErr: errors.New("delivery unavailable"), want: "delivery unavailable"},
		{name: "initialization failed", initErr: errors.New("provider failed"), want: "provider failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previous := append([]auth.AuthService(nil), auth.GetAuthServices()...)
			for _, service := range previous {
				auth.RemoveAuthService(service.AuthType())
			}
			t.Cleanup(func() {
				auth.RemoveAuthService("custom")
				for _, service := range previous {
					auth.Register(service)
				}
			})
			provider := &setupAuthService{initErr: tc.initErr}
			verifier := &verifyingSetupAuthService{setupAuthService: provider, validationErr: tc.validationErr}
			if tc.capable {
				auth.Register(verifier)
			} else {
				auth.Register(provider)
			}
			cfg := &config.Config{Auth: config.AuthConfig{
				Enabled: true, RegistrationEnabled: tc.registration, RegisterRequiresVerification: tc.verification,
				JwtKey: "test-auth-signing-key", VerificationTokenExpiration: time.Hour, ResetTokenExpiration: time.Hour,
				Services: map[string]map[string]string{"custom": {"enabled": "true"}},
			}}
			cfg.HTTP.Session.Types = map[string]string{"default": "sqliteStore"}
			err := SetupAuthentication(&server.ApplicationServer{Config: cfg, Web: http.NewServeMux()})
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
		})
	}
}
