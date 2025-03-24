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

// BuildUrl builds a URL from the consistuent parts provided in the parameter list
func BuildUrl(r *http.Request, host string, port string, path string, params map[string]string) (*url.URL, error) {
	if host == "" {
		return nil, errors.New("host cannot be empty")
	}
	if r == nil {
		return nil, errors.New("request cannot be nil")
	}

	scheme := "http"
	if forwardedProto := r.Header.Get("X-Forwarded-Proto"); forwardedProto != "" {
		scheme = forwardedProto
	} else if r.TLS != nil {
		scheme = "https"
	}

	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}

	hostPort := net.JoinHostPort(host, port)

	u := &url.URL{
		Scheme: scheme,
		Host:   hostPort,
		Path:   path,
	}

	// Add query parameters
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()

	return u, nil
}
