package session

import (
	"context"
	"fmt"
	"maps"
	"sort"
	"strings"

	"github.com/trentnix/hyperserver/config"
)

// ConfiguredProvider adapts a store to configuration-driven setup and maintenance.
// Factories receive private option maps. Open follows StoreFactory's ownership
// contract. OpenMaintenance follows MaintenanceFactory's contract. Validate must
// perform no I/O. A nil OpenMaintenance means maintenance is unsupported.
type ConfiguredProvider struct {
	Validate        func(map[string]string) error
	Open            func(context.Context, map[string]string) (SessionStore, error)
	OpenMaintenance func(context.Context, map[string]string) (ExpiredSessionMaintenance, func() error, error)
}

// NewConfiguredManager validates configuration and opens only selected providers.
// Providers contain application-supplied factories, not live services. The returned
// manager owns opened stores and must be closed after consumers stop.
func NewConfiguredManager(ctx context.Context, cfg *config.Config, providers map[string]ConfiguredProvider) (*SessionManager, error) {
	if cfg == nil {
		return nil, fmt.Errorf("session configuration is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := ValidateConfiguredStores(cfg, providers); err != nil {
		return nil, err
	}
	cfgCopy := cfg.Clone()
	cfg = &cfgCopy
	factories := make(map[string]StoreFactory)
	for name, provider := range providers {
		factories[name] = func(ctx context.Context) (SessionStore, error) {
			options, err := selectedSessionOptions(cfg, name)
			if err != nil {
				return nil, err
			}
			return provider.Open(ctx, options)
		}
	}
	return NewSessionManager(ctx, cfg.HTTP.Session.Types, factories)
}

// CleanupConfiguredStores opens maintenance storage for selected providers without
// creating tables or requiring signing keys. Each provider owns its storage setup.
func CleanupConfiguredStores(ctx context.Context, cfg *config.Config, providers map[string]ConfiguredProvider, limit int) (int64, error) {
	if cfg == nil {
		return 0, fmt.Errorf("session configuration is required")
	}
	normalized, err := normalizeProviders(providers)
	if err != nil {
		return 0, err
	}
	providers = normalized
	cfgCopy := cfg.Clone()
	cfg = &cfgCopy
	factories := make(map[string]MaintenanceFactory)
	for name, provider := range providers {
		factories[name] = func(ctx context.Context) (ExpiredSessionMaintenance, func() error, error) {
			options, err := selectedSessionOptions(cfg, name)
			if err != nil {
				return nil, nil, err
			}
			if provider.OpenMaintenance == nil {
				return nil, nil, fmt.Errorf("session provider %q does not support maintenance", name)
			}
			return provider.OpenMaintenance(ctx, options)
		}
	}
	return CleanupExpired(ctx, cfg.HTTP.Session.Types, factories, limit)
}

func selectedSessionOptions(cfg *config.Config, name string) (map[string]string, error) {
	var options map[string]string
	found := false
	for key, values := range cfg.HTTP.Session.Stores {
		if strings.EqualFold(key, name) {
			if found {
				return nil, fmt.Errorf("duplicate session store %q", name)
			}
			options, found = values, true
		}
	}
	if !found {
		return nil, fmt.Errorf("selected session store %q is not configured", name)
	}
	enabled, err := config.ProviderEnabled("http.session.stores."+name+".enabled", options["enabled"])
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, ErrStoreDisabled
	}
	return maps.Clone(options), nil
}

// ValidateConfiguredStores checks store selection without opening databases or creating sessions.
func ValidateConfiguredStores(c *config.Config, providers map[string]ConfiguredProvider) error {
	if c == nil {
		return fmt.Errorf("session configuration is required")
	}

	normalized, err := normalizeProviders(providers)
	if err != nil {
		return err
	}
	providers = normalized
	stores := make(map[string]bool)
	names := make([]string, 0, len(c.HTTP.Session.Stores))
	for name := range c.HTTP.Session.Stores {
		names = append(names, name)
	}
	sort.Strings(names)

	active := false
	for _, name := range names {
		options := c.HTTP.Session.Stores[name]
		key := strings.ToLower(name)
		if _, exists := stores[key]; exists {
			return fmt.Errorf("duplicate session store %q", name)
		}
		enabled, err := config.ProviderEnabled("http.session.stores."+name+".enabled", options["enabled"])
		if err != nil {
			return err
		}
		stores[key] = enabled
		if !enabled {
			continue
		}
		active = true
		provider, ok := providers[key]
		if !ok {
			return fmt.Errorf("unknown enabled session store %q", name)
		}
		if provider.Validate != nil {
			if err := provider.Validate(maps.Clone(options)); err != nil {
				return fmt.Errorf("http.session.stores.%s: %w", name, err)
			}
		}
	}

	names = names[:0]
	for name := range c.HTTP.Session.Types {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		provider := c.HTTP.Session.Types[name]
		if !stores[strings.ToLower(provider)] {
			return fmt.Errorf("http.session.types.%s selects unknown or disabled store %q", name, provider)
		}
	}

	if !active {
		return nil
	}
	if c.HTTP.Session.Types["default"] == "" {
		return fmt.Errorf("http.session.types.default is required when a session store is enabled")
	}

	return nil
}

func normalizeProviders(providers map[string]ConfiguredProvider) (map[string]ConfiguredProvider, error) {
	result := make(map[string]ConfiguredProvider, len(providers))
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		key := strings.ToLower(name)
		if key == "" || strings.TrimSpace(key) != key || providers[name].Open == nil {
			return nil, fmt.Errorf("invalid session provider %q", name)
		}
		if _, exists := result[key]; exists {
			return nil, fmt.Errorf("duplicate session provider %q", name)
		}
		result[key] = providers[name]
	}
	return result, nil
}
