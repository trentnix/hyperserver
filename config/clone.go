package config

import (
	"maps"
	"slices"
)

// Clone returns an independent copy, including nested maps, slices, and settings
// pointers. The source must not be mutated concurrently while it is being copied.
func (c Config) Clone() Config {
	c.HTTP.TrustedProxies = slices.Clone(c.HTTP.TrustedProxies)
	c.HTTP.Session.Types = maps.Clone(c.HTTP.Session.Types)
	c.HTTP.Session.Stores = cloneOptions(c.HTTP.Session.Stores)
	c.Auth.Services = cloneOptions(c.Auth.Services)
	c.Auth.AccountStorage.Options = maps.Clone(c.Auth.AccountStorage.Options)
	if c.Auth.RateLimit != nil {
		limit := *c.Auth.RateLimit
		c.Auth.RateLimit = &limit
	}
	if c.App.SiteRateLimit != nil {
		limit := *c.App.SiteRateLimit
		c.App.SiteRateLimit = &limit
	}
	return c
}

func cloneOptions(options map[string]map[string]string) map[string]map[string]string {
	result := maps.Clone(options)
	for name, values := range result {
		result[name] = maps.Clone(values)
	}
	return result
}
