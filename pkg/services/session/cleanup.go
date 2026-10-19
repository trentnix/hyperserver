package session

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// MaintenanceFactory opens existing storage without creating or migrating it.
// A nil interface means that no cleanup is needed. Typed nils are invalid.
// A nil factory means maintenance is unsupported. CleanupExpired calls the returned
// close function even when opening fails. Factories must honor ctx and release only
// owned resources.
type MaintenanceFactory func(context.Context) (ExpiredSessionMaintenance, func() error, error)

// CleanupExpired removes one bounded batch from each selected provider.
// Names are case-insensitive. Aliases share one batch. Unselected factories are
// not called. Independent providers are still attempted after a failure.
func CleanupExpired(ctx context.Context, types map[string]string, factories map[string]MaintenanceFactory, limit int) (count int64, err error) {
	if limit < 1 {
		return 0, errors.New("session cleanup limit must be positive")
	}
	providers := make(map[string]MaintenanceFactory, len(factories))
	for name, factory := range factories {
		key := strings.ToLower(name)
		if name == "" || strings.TrimSpace(name) != name {
			return 0, errors.New("session provider name must not be empty or have surrounding whitespace")
		}
		if _, exists := providers[key]; exists {
			return 0, fmt.Errorf("duplicate session store %q", name)
		}
		providers[key] = factory
	}

	selected := make(map[string]bool)
	for _, name := range types {
		selected[strings.ToLower(name)] = true
	}
	for _, name := range slices.Sorted(maps.Keys(selected)) {
		if contextErr := ctx.Err(); contextErr != nil {
			return count, errors.Join(err, contextErr)
		}
		n, batchErr := cleanupProvider(ctx, providers[name], limit)
		count += n
		if batchErr != nil {
			err = errors.Join(err, fmt.Errorf("clean up session store %q: %w", name, batchErr))
		}
	}
	return count, err
}

func cleanupProvider(ctx context.Context, open MaintenanceFactory, limit int) (count int64, err error) {
	if open == nil {
		return 0, errors.New("provider does not support session maintenance")
	}
	maintenance, closeStore, err := open(ctx)
	if closeStore != nil {
		defer func() { err = errors.Join(err, closeStore()) }()
	}
	if err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if maintenance == nil {
		return 0, nil
	}
	if isNilService(maintenance) {
		return 0, errors.New("provider returned nil session maintenance")
	}
	return maintenance.DeleteExpired(ctx, limit)
}
