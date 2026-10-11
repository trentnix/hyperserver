package auth

import (
	"fmt"
	"sort"
	"strings"

	"github.com/trentnix/hyperserver/config"
)

// ValidateConfig checks enabled providers against the supplied catalog.
func ValidateConfig(c *config.Config, descriptors []Descriptor) error {
	known := make(map[string]bool)
	for _, d := range descriptors {
		name := strings.ToLower(d.Name)
		if name == "" || strings.TrimSpace(name) != name || d.New == nil {
			return fmt.Errorf("invalid auth descriptor %q", d.Name)
		}
		if known[name] {
			return fmt.Errorf("duplicate registered auth service %q", name)
		}
		known[name] = true
	}
	if !c.Auth.Enabled {
		return nil
	}
	if err := c.Validate(); err != nil {
		return err
	}

	names := make([]string, 0, len(c.Auth.Services))
	for name := range c.Auth.Services {
		names = append(names, name)
	}
	sort.Strings(names)

	seen := make(map[string]bool)
	active := false
	for _, name := range names {
		key := strings.ToLower(name)
		if seen[key] {
			return fmt.Errorf("duplicate configured auth service %q", name)
		}
		seen[key] = true
		enabled, err := config.ProviderEnabled("auth.services."+name+".enabled", c.Auth.Services[name]["enabled"])
		if err != nil {
			return err
		}
		if enabled {
			if !known[key] {
				return fmt.Errorf("unknown enabled auth service %q: import its provider package", name)
			}
			active = true
		}
	}

	if !active {
		return fmt.Errorf("auth.enabled requires at least one enabled, registered auth service")
	}
	return nil
}
