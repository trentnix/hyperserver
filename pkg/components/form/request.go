package form

import (
	"errors"
	"mime"
	"net/http"
)

const (
	// MaxQueryBytes limits the encoded query string parsed with a form.
	MaxQueryBytes = 64 * 1024
	// MaxFieldBytes limits each decoded form name and value, measured in bytes.
	MaxFieldBytes = 16 * 1024
)

// ParseError describes a rejected form without retaining private input or parser details.
type ParseError struct {
	StatusCode int
	Message    string
}

// Error returns the public rejection message.
func (e *ParseError) Error() string { return e.Message }

// ParseOptions overrides decoded field limits. The zero value uses safe defaults.
// Options do not change body limits or the encoded query limit.
type ParseOptions struct {
	// MaxFieldBytes is the limit per name and value. Zero uses the package default.
	MaxFieldBytes int64
	// UnlimitedFields explicitly disables decoded field limits.
	UnlimitedFields bool
}

// Parse parses a URL-encoded form and checks every decoded name and value,
// including repeated fields and query parameters, before binding or validation.
// Body limits belong to the HTTP stack. Multipart uploads need their own parser.
func Parse(r *http.Request) *ParseError {
	return ParseWithOptions(r, ParseOptions{})
}

// ParseWithOptions parses a form with explicitly selected field limits.
func ParseWithOptions(r *http.Request, options ParseOptions) *ParseError {
	if options.MaxFieldBytes < 0 {
		return &ParseError{http.StatusInternalServerError, "Invalid form field limit"}
	}
	if len(r.URL.RawQuery) > MaxQueryBytes {
		return &ParseError{http.StatusRequestEntityTooLarge, "Form data is too large"}
	}
	contentType := r.Header.Get("Content-Type")
	if contentType != "" {
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil {
			return &ParseError{http.StatusBadRequest, "Unable to parse form data"}
		}
		if mediaType != "application/x-www-form-urlencoded" {
			return &ParseError{http.StatusUnsupportedMediaType, "Expected a URL-encoded form"}
		}
	} else if r.ContentLength != 0 {
		return &ParseError{http.StatusUnsupportedMediaType, "Expected a URL-encoded form"}
	}
	if err := r.ParseForm(); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return &ParseError{http.StatusRequestEntityTooLarge, "Form data is too large"}
		}
		return &ParseError{http.StatusBadRequest, "Unable to parse form data"}
	}

	maxBytes := options.MaxFieldBytes
	if maxBytes == 0 {
		maxBytes = MaxFieldBytes
	}
	if !options.UnlimitedFields {
		for name, values := range r.Form {
			if int64(len(name)) > maxBytes {
				return &ParseError{http.StatusRequestEntityTooLarge, "Form field is too large"}
			}
			for _, value := range values {
				if int64(len(value)) > maxBytes {
					return &ParseError{http.StatusRequestEntityTooLarge, "Form field is too large"}
				}
			}
		}
	}
	return nil
}
