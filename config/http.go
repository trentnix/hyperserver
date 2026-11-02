package config

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

// FormActionSources validates exact origins for CSP. Wildcards, directives,
// credentials, and paths are not allowed. This setting never changes CSRF policy.
func (c HTTPConfig) FormActionSources() ([]string, error) {
	var sources []string
	for i, value := range c.FormActionOrigins {
		origin, err := ParsePublicOrigin(value)
		if err != nil || origin == nil || strings.ContainsAny(value, ",;*'\" \\\t\r\n") {
			return nil, fmt.Errorf("http.formActionOrigins[%d] must be an exact HTTP or HTTPS origin", i)
		}
		sources = append(sources, origin.String())
	}
	return sources, nil
}

// ParsePublicOrigin validates the public address used for absolute links.
// An empty value leaves origin selection to the direct-connection fallback.
func ParsePublicOrigin(value string) (*url.URL, error) {
	if value == "" {
		return nil, nil
	}
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" ||
		u.User != nil || u.Opaque != "" || (u.Path != "" && u.Path != "/") ||
		u.RawQuery != "" || u.ForceQuery || strings.Contains(value, "#") || strings.HasSuffix(u.Host, ":") {
		return nil, errors.New("http.publicOrigin must be an absolute HTTP or HTTPS origin without credentials, a path, query, or fragment")
	}
	if strings.HasPrefix(u.Host, "[") {
		address, err := netip.ParseAddr(u.Hostname())
		if err != nil || !address.Is6() {
			return nil, errors.New("http.publicOrigin brackets must contain an IPv6 address")
		}
	} else if strings.ContainsAny(u.Hostname(), ":[]") {
		return nil, errors.New("http.publicOrigin has an invalid host or port. IPv6 addresses must use brackets")
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return nil, errors.New("http.publicOrigin port must be between 1 and 65535")
		}
	}
	u.Path = ""
	return u, nil
}
