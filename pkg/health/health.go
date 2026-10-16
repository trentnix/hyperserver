// Package health supplies HTTP probes with application-selected readiness checks.
package health

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/trentnix/hyperserver/pkg/services/logger"
)

// Check tests a required capability without changing data. Run must honor ctx
// and support concurrent calls.
// Name must be a static, non-secret label. Checks run in the supplied order.
type Check struct {
	Name string
	Run  func(context.Context) error
}

// Liveness reports that the HTTP process can respond, without probing services.
func Liveness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

// Readiness checks only the capabilities selected by the application. All checks
// share one positive timeout. A failed check returns a generic 503 and logs only
// its name, never provider error text. Checks must cooperate with cancellation.
// Use a check of the application's run context to report shutdown as not ready.
func Readiness(timeout time.Duration, checks ...Check) (http.Handler, error) {
	if timeout <= 0 {
		return nil, fmt.Errorf("readiness timeout must be positive")
	}
	seen := make(map[string]bool)
	for _, check := range checks {
		if check.Name == "" || check.Run == nil || seen[check.Name] {
			return nil, fmt.Errorf("readiness checks require unique names and functions")
		}
		seen[check.Name] = true
	}
	checks = append([]Check(nil), checks...)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		if ctx.Err() != nil {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		for _, check := range checks {
			if err := check.Run(ctx); err != nil || ctx.Err() != nil {
				if l := logger.Get(r.Context()); l != nil {
					l.WarnContext(r.Context(), "Readiness failed", "check", check.Name)
				}
				http.Error(w, "not ready", http.StatusServiceUnavailable)
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}), nil
}
