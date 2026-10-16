package middleware

import (
	"bufio"
	"context"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/trentnix/hyperserver/pkg/services/logger"
)

// LoggerMiddleware logs method, path, response status, and elapsed time.
// It excludes query parameters, headers, and bodies. Applications must not put
// credentials in URL paths or log fields. Panics propagate to net/http.
// It adds a logger and tracing ID to the request context and returns the ID in
// X-Request-ID. Handlers receive the original request URL unchanged.
func LoggerMiddleware(l *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			requestId := getRequestIdentifier()
			ctx := context.WithValue(r.Context(), logger.RequestIDKey, requestId)
			var reqLogger *slog.Logger
			if l != nil {
				reqLogger = l.With(
					slog.Any(string(logger.RequestIDKey), requestId),
					slog.Any("method", r.Method),
					slog.Any("path", r.URL.Path),
				)

				ctx = logger.Set(ctx, reqLogger)
			} else {
				ctx = logger.Set(ctx, nil)
			}

			// set it in the response header for client visibility
			w.Header().Set("X-Request-ID", requestId)
			if reqLogger == nil {
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			response := &loggedResponse{ResponseWriter: w}
			completed := false
			defer func() {
				status := response.status
				if completed && status == 0 && !response.hijacked {
					status = http.StatusOK
				}
				reqLogger.InfoContext(ctx, "Request completed", "status", status,
					"duration", time.Since(start), "aborted", !completed, "hijacked", response.hijacked)
			}()
			next.ServeHTTP(response, r.WithContext(ctx))
			completed = true
		})
	}
}

type loggedResponse struct {
	http.ResponseWriter
	status   int
	hijacked bool
}

func (w *loggedResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *loggedResponse) WriteHeader(status int) {
	w.ResponseWriter.WriteHeader(status)
	if w.status == 0 && (status >= 200 || status == http.StatusSwitchingProtocols) {
		w.status = status
	}
}

func (w *loggedResponse) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func (w *loggedResponse) FlushError() error {
	err := http.NewResponseController(w.ResponseWriter).Flush()
	if err == nil && w.status == 0 {
		w.status = http.StatusOK
	}
	return err
}

func (w *loggedResponse) Flush() { _ = w.FlushError() }

func (w *loggedResponse) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		w.hijacked = true
	}
	return conn, rw, err
}

func (w *loggedResponse) Push(target string, options *http.PushOptions) error {
	if pusher, ok := w.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, options)
	}
	return http.ErrNotSupported
}

// getRequestIdentifier returns a newly created UUID value that will serve as a request
// identifier for tracing requests through the application
func getRequestIdentifier() string {
	return uuid.New().String()
}
