// Package routing registers ordinary net/http handlers with body and rate policies.
// It delegates matching and dispatch to http.ServeMux.
package routing

import (
	"net/http"

	"github.com/trentnix/hyperserver/pkg/ratelimit"
)

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
	mux        *http.ServeMux
	ratePolicy *ratelimit.Policy

	// OnRateLimitRegistered receives a copy of each limited route's final policy
	// after successful registration. Set it before creating child scopes, which
	// inherit the callback. Unlimited routes do not invoke it. It is intended for
	// startup diagnostics and is never called while handling requests.
	OnRateLimitRegistered func(pattern string, policy ratelimit.Policy)
}

// NewRoutes adds body-limited registration to mux without replacing it.
// No rate limit applies until WithRateLimit selects a policy.
// Register application routes through Routes. Direct mux.Handle calls bypass limits.
func NewRoutes(mux *http.ServeMux) *Routes {
	return &Routes{mux: mux}
}

// WithRateLimit returns a registration scope with a per-route default. It does
// not modify the parent scope or routes already registered. A nested scope
// replaces the parent's policy, not its counters. Policy is copied and validated
// here. Every Handle call creates one independent limiter for its route pattern.
func (r *Routes) WithRateLimit(policy ratelimit.Policy) (*Routes, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	scope := *r
	scope.ratePolicy = &policy
	return &scope, nil
}

// WithoutRateLimit returns a scope with no inherited route limit. Body limits
// and explicitly attached shared budgets remain active.
func (r *Routes) WithoutRateLimit() *Routes {
	scope := *r
	scope.ratePolicy = nil
	return &scope
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
	r.register(pattern, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// Preserve request identity for handlers that update request-local state.
		req.Body = http.MaxBytesReader(w, req.Body, maxBytes)
		handler.ServeHTTP(w, req)
	}))
}

// HandleWithoutBodyLimit registers handler without a request-body limit.
// Prefer a finite limit whenever possible.
func (r *Routes) HandleWithoutBodyLimit(pattern string, handler http.Handler) {
	r.register(pattern, handler)
}

func (r *Routes) register(pattern string, handler http.Handler) {
	if r.ratePolicy != nil {
		// WithRateLimit validated this private policy snapshot. The name uses the
		// registered pattern, never the request path or submitted values.
		limiter, _ := ratelimit.New("route "+pattern, *r.ratePolicy)
		handler = limiter.Handler(handler)
	}
	r.mux.Handle(pattern, handler)
	if r.ratePolicy != nil && r.OnRateLimitRegistered != nil {
		r.OnRateLimitRegistered(pattern, *r.ratePolicy)
	}
}
