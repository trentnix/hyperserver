// util.go contains utility functions that are used in the application
package util

import (
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/trentnix/hyperserver/config"
)

// GetWorkingDirectory will retrieve the current working directory
func GetWorkingDirectory() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", NewErrFailedToGetWorkingDirectory(nil)
	}

	return wd, nil
}

// SetWorkingDirectory will set the application's working directory to the specified path
func SetWorkingDirectory(wd string) error {
	if wd == "" {
		return NewErrWorkingDirectoryNotSpecified(fmt.Errorf("SetWorkingDirectory"))
	}

	if err := os.Chdir(wd); err != nil {
		return NewErrFailedToSetWorkingDirectory(err)
	}

	return nil
}

// LoadHTMLFromFile takes the file at the specified filePath and returns a template.HTML object with
// its contents
func LoadHTMLFromFile(filePath string) (template.HTML, error) {
	htmlBytes, err := os.ReadFile(filePath)
	if err != nil {
		return "", NewErrLoadingTemplate(err, filePath)
	}

	return template.HTML(htmlBytes), nil
}

// BuildPublicURL builds an absolute link using the application's public origin.
// Without one, it uses the listener settings and the connection's TLS state.
// Request host and forwarded headers are ignored.
func BuildPublicURL(r *http.Request, cfg config.HTTPConfig, path string, params map[string]string) (*url.URL, error) {
	origin, err := config.ParsePublicOrigin(cfg.PublicOrigin)
	if err != nil {
		return nil, err
	}
	if origin == nil {
		if cfg.Port == 0 {
			return nil, errors.New("http.port must be nonzero to build a link without http.publicOrigin")
		}
		if r == nil {
			return nil, errors.New("request cannot be nil")
		}

		host := cfg.Hostname
		if host == "" {
			host = "localhost"
		}
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}

		if (scheme == "http" && cfg.Port == 80) || (scheme == "https" && cfg.Port == 443) {
			if strings.Contains(host, ":") {
				host = "[" + host + "]"
			}
		} else {
			host = net.JoinHostPort(host, strconv.Itoa(int(cfg.Port)))
		}
		origin = &url.URL{Scheme: scheme, Host: host}
	}

	origin.Path = path
	query := url.Values{}
	for key, value := range params {
		query.Set(key, value)
	}
	origin.RawQuery = query.Encode()
	return origin, nil
}

// IsValidUri validates that the specified URI has a valid value
func IsValidUri(uri string, requireSchemeAndHost bool) error {
	u, err := url.ParseRequestURI(uri)
	if err != nil {
		return NewErrInvalidUri(err, uri)
	}

	if requireSchemeAndHost {
		if u.Scheme == "" || u.Host == "" {
			return NewErrInvalidUri(errors.New("the scheme value or host value is empty"), uri)
		}
	}

	return nil
}
