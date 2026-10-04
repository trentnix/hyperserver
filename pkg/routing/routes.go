// Package routing registers ordinary net/http handlers with default body limits.
// It delegates matching and dispatch to http.ServeMux.
package routing

import "net/http"

const (
	// KiB is 1,024 bytes.
	KiB = 1024
	// MiB is 1,048,576 bytes.
	MiB = 1024 * KiB
	// DefaultMaxBodyBytes applies to routes registered with Handle or HandleFunc.
	DefaultMaxBodyBytes int64 = 64 * KiB
)

// Routes registers handlers on a ServeMux. Register routes before serving requests.
// Body limits bound reads, not content types. Handlers must check read errors
// before changing data and report *http.MaxBytesError as HTTP 413.
type Routes struct {
	mux *http.ServeMux
}

// NewRoutes adds default-limited registration to mux without replacing it.
// Register application routes through Routes. Direct mux.Handle calls bypass limits.
func NewRoutes(mux *http.ServeMux) *Routes {
	return &Routes{mux: mux}
}

// Handle registers handler with the default request-body limit.
func (r *Routes) Handle(pattern string, handler http.Handler) {
	r.HandleWithBodyLimit(pattern, handler, DefaultMaxBodyBytes)
}

// HandleFunc registers a function with the default request-body limit.
func (r *Routes) HandleFunc(pattern string, handler http.HandlerFunc) {
	r.Handle(pattern, handler)
}

// HandleWithBodyLimit registers handler with one explicit limit instead of the
// default. Like http.MaxBytesReader, zero and negative limits allow no body bytes.
// Use HandleWithoutBodyLimit to explicitly disable the limit.
func (r *Routes) HandleWithBodyLimit(pattern string, handler http.Handler, maxBytes int64) {
	r.mux.Handle(pattern, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// Preserve request identity for handlers that update request-local state.
		req.Body = http.MaxBytesReader(w, req.Body, maxBytes)
		handler.ServeHTTP(w, req)
	}))
}

// HandleWithoutBodyLimit registers handler without a request-body limit.
// Prefer a finite limit whenever possible.
func (r *Routes) HandleWithoutBodyLimit(pattern string, handler http.Handler) {
	r.mux.Handle(pattern, handler)
}
