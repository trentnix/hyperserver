// Package util provides URL, template-file, and HTTP response helpers.
// BuildPublicURL uses configured addresses rather than request host or forwarded
// headers. RedirectToURL accepts only local destinations. LoadHTML
// requires trusted input.
package util

import (
	"errors"
	"html/template"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/trentnix/hyperserver/config"
)

// LoadHTML reads a trusted file from files and marks its contents as template.HTML.
// It does not parse or escape the file's contents.
func LoadHTML(files fs.FS, filePath string) (template.HTML, error) {
	if files == nil {
		return "", NewErrLoadingTemplate(errors.New("template filesystem is required"), filePath)
	}
	htmlBytes, err := fs.ReadFile(files, filePath)
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

		host := strings.TrimSpace(cfg.ListenHost)
		if host == "" {
			host = "127.0.0.1"
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
			return nil, errors.New("http.publicOrigin is required to build a link when http.listenHost is a wildcard address")
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
