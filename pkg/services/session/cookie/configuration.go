package cookie

import (
	"context"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/session"
)

// ConfiguredProvider adapts cookie sessions to configuration-driven setup.
// Construction performs no I/O. Cookie sessions require no storage maintenance.
func ConfiguredProvider(cfg config.Config) session.ConfiguredProvider {
	settings := cfg.HTTP.Session
	return session.ConfiguredProvider{
		Open: func(ctx context.Context, _ map[string]string) (session.SessionStore, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return NewCookieStore(Options{
				SigningKey:     settings.JwtKey,
				TokenLifetime:  settings.TokenAge,
				CookieLifetime: settings.CookieAge,
			})
		},
		OpenMaintenance: func(context.Context, map[string]string) (session.ExpiredSessionMaintenance, func() error, error) {
			return nil, nil, nil
		},
	}
}
