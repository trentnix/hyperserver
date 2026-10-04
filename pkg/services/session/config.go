package session

import (
	"fmt"
	"sort"
	"strings"

	"github.com/trentnix/hyperserver/config"
)

// ValidateConfig checks store selection without opening databases or creating sessions.
func ValidateConfig(c *config.Config) error {
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
		switch key {
		case strings.ToLower(cookieStoreName):
		case strings.ToLower(sqliteStoreName):
			if strings.TrimSpace(options["connection"]) == "" || strings.TrimSpace(options["sessiontable"]) == "" {
				return fmt.Errorf("http.session.stores.%s requires connection and sessionTable", name)
			}
		default:
			return fmt.Errorf("unknown enabled session store %q", name)
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
	if c.HTTP.Session.Types[defaultStore] == "" {
		return fmt.Errorf("http.session.types.default is required when a session store is enabled")
	}

	if err := config.ValidateSigningKey("http.session.jwtKey", c.HTTP.Session.JwtKey); err != nil {
		return err
	}
	if err := config.ValidateLifetime("http.session.tokenAge", c.HTTP.Session.TokenAge); err != nil {
		return err
	}
	return config.ValidateLifetime("http.session.cookieAge", c.HTTP.Session.CookieAge)
}

// ValidateRotationConfig checks whether the selected provider supports revocable
// rotation without opening a store. ValidateConfig must also validate provider options.
func ValidateRotationConfig(c *config.Config, name string) error {
	provider, ok := c.HTTP.Session.Types[name]
	if !ok {
		provider = c.HTTP.Session.Types[defaultStore]
	}
	if !strings.EqualFold(provider, sqliteStoreName) {
		return fmt.Errorf("http.session.types.%s selects store %q, which does not support revocable session rotation", name, provider)
	}
	return nil
}
