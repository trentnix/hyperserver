package config

import "errors"

// ModuleRateLimitConfig sets the client capacity of each route using a module's
// policy. It does not change the module's request allowance or window duration.
type ModuleRateLimitConfig struct {
	MaxClients int // Maximum tracked client IPs per route. Must be positive.
}

// ClientCapacity returns the configured capacity. A nil configuration uses
// 4,096 clients. An explicit zero or negative capacity is invalid.
func (c *ModuleRateLimitConfig) ClientCapacity() (int, error) {
	if c == nil {
		return 4096, nil
	}
	if c.MaxClients <= 0 {
		return 0, errors.New("maxClients must be positive")
	}
	return c.MaxClients, nil
}
