package configured_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/configured"
	"github.com/trentnix/hyperserver/pkg/services/session"
)

type sessionMaintenanceFunc func(context.Context, int) (int64, error)

func (f sessionMaintenanceFunc) DeleteExpired(ctx context.Context, limit int) (int64, error) {
	return f(ctx, limit)
}

func TestSessionMaintenanceSelectsOnlyConfiguredProviders(t *testing.T) {
	for _, name := range []string{"cookiestore", "sqlitestore", "custom", "unknown", "unselected"} {
		t.Run(name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.HTTP.Session.Types = map[string]string{"default": name, "alias": strings.ToUpper(name)}
			cfg.HTTP.Session.Stores = map[string]map[string]string{name: {"enabled": "true", "location": "original"}}
			if name == "unselected" {
				cfg.HTTP.Session.Types = nil
			}
			opens, closes := 0, 0
			provider := session.ConfiguredProvider{
				Validate: func(map[string]string) error {
					t.Fatal("maintenance ran runtime validation")
					return nil
				},
				OpenMaintenance: func(ctx context.Context, options map[string]string) (session.ExpiredSessionMaintenance, func() error, error) {
					opens++
					if options["location"] != "original" {
						t.Fatal("lost provider options")
					}
					options["location"] = "changed"
					return sessionMaintenanceFunc(func(got context.Context, limit int) (int64, error) {
						if got != ctx || got != t.Context() || limit != 3 {
							t.Fatal("lost context or batch limit")
						}
						return 2, nil
					}), func() error { closes++; return nil }, nil
				},
			}
			providers := map[string]session.ConfiguredProvider{"cookiestore": provider, "sqlitestore": provider, "custom": provider}
			count, err := configured.CleanupSessions(t.Context(), cfg, 3, providers)
			wantCalls := 1
			if name == "unknown" || name == "unselected" {
				wantCalls = 0
			}
			if (err != nil) != (name == "unknown") || count != int64(2*wantCalls) || opens != wantCalls || closes != wantCalls {
				t.Fatalf("count=%d opens=%d closes=%d error=%v", count, opens, closes, err)
			}
			if cfg.HTTP.Session.Stores[name]["location"] != "original" || len(providers) != 3 {
				t.Fatal("maintenance changed caller configuration or catalog")
			}
		})
	}
}

func TestSessionMaintenanceFailures(t *testing.T) {
	for _, stage := range []string{"open", "delete", "close", "canceled", "invalid limit", "unsupported"} {
		t.Run(stage, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.HTTP.Session.Types = map[string]string{"default": "custom"}
			cfg.HTTP.Session.Stores = map[string]map[string]string{"custom": {"enabled": "true"}}
			cause, closeErr := errors.New("maintenance failed"), errors.New("close failed")
			opens, closes := 0, 0
			provider := session.ConfiguredProvider{
				Open: func(context.Context, map[string]string) (session.SessionStore, error) {
					t.Fatal("maintenance opened runtime storage")
					return nil, nil
				},
				OpenMaintenance: func(context.Context, map[string]string) (session.ExpiredSessionMaintenance, func() error, error) {
					opens++
					closeStore := func() error { closes++; return closeErr }
					if stage == "open" {
						return nil, closeStore, cause
					}
					return sessionMaintenanceFunc(func(context.Context, int) (int64, error) {
						if stage == "delete" {
							return 2, cause
						}
						return 2, nil
					}), closeStore, nil
				},
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			limit, wantCalls, wantCount := 3, 1, int64(2)
			switch stage {
			case "open":
				wantCount = 0
			case "canceled":
				cancel()
				wantCalls, wantCount = 0, 0
			case "invalid limit":
				limit, wantCalls, wantCount = 0, 0, 0
			case "unsupported":
				provider.OpenMaintenance = nil
				wantCalls, wantCount = 0, 0
			}
			count, err := configured.CleanupSessions(ctx, cfg, limit, map[string]session.ConfiguredProvider{"custom": provider})
			if err == nil || count != wantCount || opens != wantCalls || closes != wantCalls {
				t.Fatalf("count=%d opens=%d closes=%d error=%v", count, opens, closes, err)
			}
			if wantCalls == 1 && !errors.Is(err, closeErr) {
				t.Fatalf("lost close error: %v", err)
			}
			if (stage == "open" || stage == "delete") && !errors.Is(err, cause) {
				t.Fatalf("lost operation error: %v", err)
			}
			if stage == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation: %v", err)
			}
		})
	}
}
