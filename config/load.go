package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// GetConfig loads the first config.yaml in ., config, ../config, or ../../config.
// Environment values override file values, which override defaults. A missing or
// invalid file returns an error. Providers validate their own settings separately.
func GetConfig() (Config, error) {
	for _, directory := range []string{".", "config", "../config", "../../config"} {
		path := filepath.Join(directory, "config.yaml")
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return Config{}, err
		}
		cfg, err := readConfig(bytes.NewReader(data), os.Environ())
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", path, err)
		}
		return cfg, nil
	}
	return Config{}, fmt.Errorf("config.yaml not found in ., config, ../config, or ../../config: %w", os.ErrNotExist)
}

func defaultConfig() Config {
	var c Config
	c.HTTP.ListenHost = "127.0.0.1"
	c.HTTP.DefaultRateLimit = RateLimitConfig{Requests: 20, Window: time.Minute, MaxClients: 4096}
	c.HTTP.SharedRateLimit = RateLimitConfig{Requests: 120, Window: time.Minute, MaxClients: 4096}
	c.Auth.RateLimit = &ModuleRateLimitConfig{MaxClients: 4096}
	c.App.SiteRateLimit = &ModuleRateLimitConfig{MaxClients: 4096}
	c.Auth.ResetMinimumResponseTime = 2 * time.Second
	return c
}

// configFields is the list of scalar and list settings accepted from either source.
// Provider maps are handled separately because their keys belong to providers.
func configFields(c *Config) map[string]any {
	return map[string]any{
		"http.publicOrigin":                 &c.HTTP.PublicOrigin,
		"http.trustedProxies":               &c.HTTP.TrustedProxies,
		"http.hstsMaxAge":                   &c.HTTP.HSTSMaxAge,
		"http.listenHost":                   &c.HTTP.ListenHost,
		"http.port":                         &c.HTTP.Port,
		"http.readTimeout":                  &c.HTTP.ReadTimeout,
		"http.writeTimeout":                 &c.HTTP.WriteTimeout,
		"http.idleTimeout":                  &c.HTTP.IdleTimeout,
		"http.defaultRateLimit.enabled":     &c.HTTP.DefaultRateLimit.Enabled,
		"http.defaultRateLimit.requests":    &c.HTTP.DefaultRateLimit.Requests,
		"http.defaultRateLimit.window":      &c.HTTP.DefaultRateLimit.Window,
		"http.defaultRateLimit.maxClients":  &c.HTTP.DefaultRateLimit.MaxClients,
		"http.sharedRateLimit.enabled":      &c.HTTP.SharedRateLimit.Enabled,
		"http.sharedRateLimit.requests":     &c.HTTP.SharedRateLimit.Requests,
		"http.sharedRateLimit.window":       &c.HTTP.SharedRateLimit.Window,
		"http.sharedRateLimit.maxClients":   &c.HTTP.SharedRateLimit.MaxClients,
		"http.tls.enabled":                  &c.HTTP.TLS.Enabled,
		"http.tls.certificate":              &c.HTTP.TLS.Certificate,
		"http.tls.key":                      &c.HTTP.TLS.Key,
		"http.session.jwtKey":               &c.HTTP.Session.JwtKey,
		"http.session.tokenAge":             &c.HTTP.Session.TokenAge,
		"http.session.cookieAge":            &c.HTTP.Session.CookieAge,
		"auth.accountStorage.provider":      &c.Auth.AccountStorage.Provider,
		"auth.rateLimit.maxClients":         &c.Auth.RateLimit.MaxClients,
		"auth.enabled":                      &c.Auth.Enabled,
		"auth.registrationEnabled":          &c.Auth.RegistrationEnabled,
		"auth.jwtKey":                       &c.Auth.JwtKey,
		"auth.verificationEndpoint":         &c.Auth.VerificationEndpoint,
		"auth.verificationTokenExpiration":  &c.Auth.VerificationTokenExpiration,
		"auth.resetTokenExpiration":         &c.Auth.ResetTokenExpiration,
		"auth.resetMinimumResponseTime":     &c.Auth.ResetMinimumResponseTime,
		"auth.resetRequiresNewCredentials":  &c.Auth.ResetRequiresNewCredentials,
		"auth.registerRequiresVerification": &c.Auth.RegisterRequiresVerification,
		"database.driver":                   &c.Database.Driver,
		"database.connection":               &c.Database.Connection,
		"database.testConnection":           &c.Database.TestConnection,
		"app.siteRateLimit.maxClients":      &c.App.SiteRateLimit.MaxClients,
		"app.name":                          &c.App.Name,
		"app.workingDirectory":              &c.App.WorkingDirectory,
		"app.renderNotifications":           &c.App.RenderNotifications,
		"mail.hostname":                     &c.Mail.Hostname,
		"mail.port":                         &c.Mail.Port,
		"mail.user":                         &c.Mail.User,
		"mail.password":                     &c.Mail.Password,
		"mail.fromAddress":                  &c.Mail.FromAddress,
		"mail.timeout":                      &c.Mail.Timeout,
	}
}

func readConfig(reader io.Reader, environment []string) (Config, error) {
	var root yaml.Node
	decoder := yaml.NewDecoder(reader)
	if err := decoder.Decode(&root); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, errors.New("configuration contains invalid YAML")
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Config{}, errors.New("configuration must contain one YAML document")
	}

	c := defaultConfig()
	fields := configFields(&c)
	values := make(map[string]yaml.Node)
	if len(root.Content) > 0 {
		var entries map[string]any
		if err := root.Decode(&entries); err != nil || entries == nil {
			return Config{}, errors.New("configuration must be a YAML mapping with unique keys")
		}
		if err := collectSettings(entries, "", values); err != nil {
			return Config{}, err
		}
	}
	for _, entry := range environment {
		name, value, _ := strings.Cut(entry, "=")
		if suffix, ok := strings.CutPrefix(name, "HYPERSERVER_"); ok {
			key := strings.ToLower(strings.ReplaceAll(suffix, "_", "."))
			values[key] = yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := assignSetting(&c, fields, key, values[key]); err != nil {
			return Config{}, err
		}
	}
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

// Normalize names after YAML has resolved aliases and checked duplicate keys.
func collectSettings(entries map[string]any, prefix string, values map[string]yaml.Node) error {
	seen := make(map[string]bool)
	for name, child := range entries {
		key := prefix + strings.ToLower(name)
		if seen[key] || name == "" || strings.Contains(name, ".") {
			return fmt.Errorf("%s: duplicate or invalid setting name", key)
		}
		seen[key] = true
		if mapping, ok := child.(map[string]any); ok {
			// Keep empty sections visible so unknown or scalar settings cannot hide in them.
			values[key] = yaml.Node{Kind: yaml.MappingNode}
			if err := collectSettings(mapping, key+".", values); err != nil {
				return err
			}
		} else {
			if _, ok := child.(map[any]any); ok {
				return fmt.Errorf("%s: setting names must be strings", key)
			}
			var node yaml.Node
			if err := node.Encode(child); err != nil {
				return fmt.Errorf("%s: invalid value", key)
			}
			values[key] = node
		}
	}
	return nil
}

func assignSetting(c *Config, fields map[string]any, key string, node yaml.Node) error {
	if key == "http.hostname" {
		return errors.New("http.hostname was renamed to http.listenHost. Rename HYPERSERVER_HTTP_HOSTNAME to HYPERSERVER_HTTP_LISTENHOST for environment configuration")
	}
	if key == "http.ratelimit" || strings.HasPrefix(key, "http.ratelimit.") {
		return errors.New("http.rateLimit is ambiguous. Use http.defaultRateLimit for inherited settings or http.sharedRateLimit for an aggregate budget")
	}
	for name, destination := range fields {
		if strings.EqualFold(key, name) {
			return decodeSetting(name, node, destination)
		}
		if node.Kind == yaml.MappingNode && strings.HasPrefix(strings.ToLower(name), key+".") {
			return nil
		}
	}
	for _, group := range []struct {
		prefix string
		values *map[string]string
		nested *map[string]map[string]string
	}{
		{"http.session.stores", nil, &c.HTTP.Session.Stores},
		{"http.session.types", &c.HTTP.Session.Types, nil},
		{"auth.services", nil, &c.Auth.Services},
		{"auth.accountstorage.options", &c.Auth.AccountStorage.Options, nil},
	} {
		if key == group.prefix && node.Kind == yaml.MappingNode {
			return nil
		}
		suffix, ok := strings.CutPrefix(key, group.prefix+".")
		if !ok {
			continue
		}
		parts := strings.Split(suffix, ".")
		destination := group.values
		if group.nested != nil {
			if len(parts) == 1 && node.Kind == yaml.MappingNode {
				if *group.nested == nil {
					*group.nested = make(map[string]map[string]string)
				}
				(*group.nested)[parts[0]] = make(map[string]string)
				return nil
			}
			if len(parts) != 2 {
				break
			}
			if *group.nested == nil {
				*group.nested = make(map[string]map[string]string)
			}
			options := (*group.nested)[parts[0]]
			if options == nil {
				options = make(map[string]string)
				(*group.nested)[parts[0]] = options
			}
			destination = &options
		} else if len(parts) != 1 {
			break
		}
		var value string
		if err := decodeSetting(key, node, &value); err != nil {
			return err
		}
		if *destination == nil {
			*destination = make(map[string]string)
		}
		(*destination)[parts[len(parts)-1]] = value
		return nil
	}
	return fmt.Errorf("%s: unknown setting or invalid section", key)
}

func decodeSetting(name string, node yaml.Node, destination any) error {
	if list, ok := destination.(*[]string); ok && node.Kind == yaml.SequenceNode {
		if err := node.Decode(list); err != nil {
			return fmt.Errorf("%s: expected a list of strings", name)
		}
		return nil
	}
	if node.Kind != yaml.ScalarNode || node.Tag == "!!null" {
		return fmt.Errorf("%s: expected a value", name)
	}

	var err error
	switch target := destination.(type) {
	case *string:
		*target = node.Value
	case *bool:
		*target, err = strconv.ParseBool(node.Value)
	case *uint16:
		var value uint64
		value, err = strconv.ParseUint(node.Value, 10, 16)
		*target = uint16(value)
	case *int:
		*target, err = strconv.Atoi(node.Value)
	case *int64:
		*target, err = strconv.ParseInt(node.Value, 10, 64)
	case *time.Duration:
		*target, err = time.ParseDuration(node.Value)
		if err != nil {
			return fmt.Errorf("%s: invalid duration (use units such as ms, s, or h, or 0)", name)
		}
	case *[]string:
		*target = nil
		if node.Value != "" {
			*target = strings.Split(node.Value, ",")
		}
	default:
		return fmt.Errorf("%s: unsupported configuration type", name)
	}
	if err != nil {
		// Conversion errors can contain secrets supplied under the wrong key.
		return fmt.Errorf("%s: invalid value", name)
	}
	return nil
}
