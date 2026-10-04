package util

import (
	"fmt"
	"net/http"

	"github.com/trentnix/hyperserver/pkg/services/logger"
)

// HttpError writes a plain-text error response with httpStatus.
// Only message is sent to the client and must be suitable for public display.
// A non-nil err is logged through the request logger, not included in the response.
// Logged errors must not contain secrets.
func HttpError(w http.ResponseWriter, r *http.Request, message string, err error, httpStatus int) {
	if err != nil {
		logger.LogRequestError(r, fmt.Errorf("%s (%d): %w", message, httpStatus, err))
	}

	http.Error(w, message, httpStatus)
}

// HttpMessage writes a text message to the ResponseWriter
func HttpMessage(w http.ResponseWriter, r *http.Request, message string) {
	httpStatus := http.StatusOK

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(httpStatus)
	_, err := w.Write([]byte(message))
	if err != nil {
		logger.LogRequestError(r, fmt.Errorf("%s (%d): %w", message, httpStatus, err))
	}
}

// HttpNotFound is a fallback when a requested resource is not found
func HttpNotFound(w http.ResponseWriter, r *http.Request) {
	httpStatus := http.StatusNotFound
	message := fmt.Sprintf("the resource requested ('%s') was not found", r.URL.Path)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(httpStatus)
	_, err := w.Write([]byte(message))
	if err != nil {
		logger.LogRequestError(r, fmt.Errorf("%s (%d): %w", message, httpStatus, err))
	}
}
