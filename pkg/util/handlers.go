package util

import (
	"fmt"
	"net/http"

	"github.com/trentnix/hyperserver/pkg/services/logger"
)

// HttpError writes a plain-text error response with httpStatus.
// When err is non-nil, it logs err and includes its message in the response.
// Do not pass internal errors containing secrets or sensitive details.
func HttpError(w http.ResponseWriter, r *http.Request, message string, err error, httpStatus int) {
	errMessage := message
	if err != nil {
		errMessage = fmt.Sprintf("%s: %v", message, err)
		logger.LogRequestError(r, fmt.Errorf("%s (%d): %w", message, httpStatus, err))
	}

	http.Error(w, errMessage, httpStatus)
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
