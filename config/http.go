package config

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
)

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
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return nil, errors.New("http.publicOrigin port must be between 1 and 65535")
		}
	}
	u.Path = ""
	return u, nil
}
