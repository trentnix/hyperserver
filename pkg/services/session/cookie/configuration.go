package cookie

import (
	"context"
	"errors"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/session/internal/sessioncookie"
)

// ConfiguredProvider adapts cookie sessions to configuration-driven setup.
// Construction performs no I/O. Cookie sessions require no storage maintenance.
// The only provider option is enabled. Cookie settings come from http.session.
func ConfiguredProvider(cfg config.Config) session.ConfiguredProvider {
	settings := cfg.HTTP.Session
	return session.ConfiguredProvider{
		Validate: func(options map[string]string) error {
			if err := validateOptions(options); err != nil {
				return err
			}
			return sessioncookie.ValidateConfig(settings.JwtKey, settings.TokenAge, settings.CookieAge)
		},
		Open: func(ctx context.Context, options map[string]string) (session.SessionStore, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if err := validateOptions(options); err != nil {
				return nil, err
			}
			return NewCookieStore(Options{
				SigningKey:     settings.JwtKey,
				TokenLifetime:  settings.TokenAge,
				CookieLifetime: settings.CookieAge,
			})
		},
		OpenMaintenance: func(_ context.Context, options map[string]string) (session.ExpiredSessionMaintenance, func() error, error) {
			return nil, nil, validateOptions(options)
		},
	}
}

func validateOptions(options map[string]string) error {
	for name := range options {
		if name != "enabled" {
			return errors.New("cookie sessions accept only the enabled option")
		}
	}
	return nil
}
