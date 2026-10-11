package handlers

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ShutdownHandler releases a module's owned resources after its consumers stop.
// It is optional. Shutdown must honor ctx, stop background work, and leave borrowed
// services open. A failed Init must release its own partially acquired resources.
type ShutdownHandler interface {
	Shutdown(context.Context) error
}

// Close shuts down initialized modules in reverse dependency order. It attempts
// every hook, even after errors or cancellation, and joins their errors. Hooks must
// honor ctx. Without a caller deadline, the whole cleanup has ten seconds.
// Call serially after requests drain. Each entry is consumed so repeated calls on
// the same slice do not run hooks again. Do not use modules after closing them.
func Close(ctx context.Context, modules []Instance) error {
	if _, bounded := ctx.Deadline(); !bounded {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
	}

	var result error
	for i := len(modules) - 1; i >= 0; i-- {
		module := modules[i]
		modules[i].Handler = nil
		if closer, ok := module.Handler.(ShutdownHandler); ok {
			if err := errors.Join(closer.Shutdown(ctx), ctx.Err()); err != nil {
				result = errors.Join(result, fmt.Errorf("shutdown module %q: %w", module.Name, err))
			}
		}
	}
	return result
}
