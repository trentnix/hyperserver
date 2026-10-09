// Package routing registers ordinary net/http handlers with body and rate policies.
// It delegates matching and dispatch to http.ServeMux.
package routing

import (
	"fmt"
	"net/http"
	"runtime"

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
// Check Err after registration and do not serve the mux if registration failed.
// Registration and error checks must run serially before serving requests.
type Routes struct {
	mux        *http.ServeMux
	ratePolicy *ratelimit.Policy
	state      *registrationState

	// OnRateLimitRegistered receives a copy of each limited route's final policy
	// after successful registration. Set it before creating child scopes, which
	// inherit the callback. Unlimited routes do not invoke it. It is intended for
	// startup diagnostics and is never called while handling requests.
	OnRateLimitRegistered func(pattern string, policy ratelimit.Policy)
}

type registrationState struct{ err error }

// NewRoutes adds body-limited registration to mux without replacing it.
// No rate limit applies until WithRateLimit selects a policy.
// Register application routes through Routes. Direct mux.Handle calls bypass limits.
func NewRoutes(mux *http.ServeMux) *Routes {
	r := &Routes{mux: mux, state: &registrationState{}}
	if mux == nil {
		r.state.err = fmt.Errorf("route registration requires a ServeMux")
	}
	return r
}

// Err returns the first registration failure across this scope and its children.
// After a failure, further registrations through these scopes do nothing.
// Earlier registrations remain in the mux. Failed startup must not serve it.
func (r *Routes) Err() error { return r.state.err }

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
// Invalid patterns, conflicts, and nil handlers are reported by Err.
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
	if !r.canRegister(pattern, handler) {
		return
	}
	r.register(pattern, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// Preserve request identity for handlers that update request-local state.
		req.Body = http.MaxBytesReader(w, req.Body, maxBytes)
		handler.ServeHTTP(w, req)
	}))
}

// HandleWithoutBodyLimit registers handler without a request-body limit.
// Prefer a finite limit whenever possible.
func (r *Routes) HandleWithoutBodyLimit(pattern string, handler http.Handler) {
	if !r.canRegister(pattern, handler) {
		return
	}
	r.register(pattern, handler)
}

func (r *Routes) canRegister(pattern string, handler http.Handler) bool {
	if r.Err() != nil {
		return false
	}
	// Check before wrapping, which would hide a nil handler from ServeMux.
	function, isFunction := handler.(http.HandlerFunc)
	if handler == nil || isFunction && function == nil {
		r.state.err = fmt.Errorf("register route %q: nil handler", pattern)
		return false
	}
	return true
}

func (r *Routes) register(pattern string, handler http.Handler) {
	if r.ratePolicy != nil {
		// WithRateLimit validated this private policy snapshot. The name uses the
		// registered pattern, never the request path or submitted values.
		limiter, _ := ratelimit.New("route "+pattern, *r.ratePolicy)
		handler = limiter.Handler(handler)
	}
	if err := registerHandler(r.mux, pattern, handler); err != nil {
		r.state.err = err
		return
	}
	if r.ratePolicy != nil && r.OnRateLimitRegistered != nil {
		r.OnRateLimitRegistered(pattern, *r.ratePolicy)
	}
}

// Recover only around ServeMux registration, never around module code, diagnostic
// callbacks, or request handling. Keep Go's own pattern and conflict rules.
func registerHandler(mux *http.ServeMux, pattern string, handler http.Handler) (err error) {
	defer func() {
		if failure := recover(); failure != nil {
			switch failure := failure.(type) {
			case runtime.Error:
				panic(failure)
			case error:
				err = fmt.Errorf("register route %q: %w", pattern, failure)
			case string:
				err = fmt.Errorf("register route %q: %s", pattern, failure)
			default:
				panic(failure)
			}
		}
	}()
	mux.Handle(pattern, handler)
	return nil
}
