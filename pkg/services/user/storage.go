package user

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"

	"github.com/trentnix/hyperserver/config"
)

// StorageProvider opens account repositories and optional token maintenance.
// Factories must honor ctx and receive private option maps. Cleanup functions
// release owned resources only and may be returned even when opening fails.
// Open returns a ready-to-use repository. Factories must validate options without
// exposing secrets in errors. OpenMaintenance must not create or migrate storage.
// A nil OpenMaintenance means maintenance is unsupported.
type StorageProvider struct {
	Open            func(context.Context, map[string]string) (AccountRepository, func() error, error)
	OpenMaintenance func(context.Context, map[string]string) (TokenMaintenance, func() error, error)
}

// OpenConfiguredStorage opens the explicitly selected provider. Names are
// case-sensitive. It closes partial resources on failure. On success, the caller
// owns the returned cleanup function and must call it after consumers stop.
// The caller controls deadlines and whether account storage is needed.
func OpenConfiguredStorage(ctx context.Context, selection config.AccountStorageConfig, providers map[string]StorageProvider) (AccountRepository, func() error, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	provider, err := selectedStorageProvider(selection.Provider, providers)
	if err != nil {
		return nil, nil, err
	}
	if provider.Open == nil {
		return nil, nil, fmt.Errorf("account provider %q has no factory", selection.Provider)
	}
	repository, cleanup, err := provider.Open(ctx, maps.Clone(selection.Options))
	err = errors.Join(err, ctx.Err())
	if err == nil && nilStorageService(repository) {
		err = errors.New("provider returned no account repository")
	}
	if err != nil {
		if cleanup != nil {
			err = errors.Join(err, cleanup())
		}
		return nil, nil, fmt.Errorf("initialize account provider %q: %w", selection.Provider, err)
	}
	return repository, cleanup, nil
}

// CleanupConfiguredTokens removes one bounded batch from the selected provider
// without opening runtime storage. Acquired maintenance resources are always
// closed, including on failure. The caller controls cancellation and deadlines.
func CleanupConfiguredTokens(ctx context.Context, selection config.AccountStorageConfig, providers map[string]StorageProvider, limit int) (count int64, err error) {
	if limit < 1 {
		return 0, errors.New("token cleanup limit must be positive")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	provider, err := selectedStorageProvider(selection.Provider, providers)
	if err != nil {
		return 0, err
	}
	if provider.OpenMaintenance == nil {
		return 0, fmt.Errorf("account provider %q does not support token maintenance", selection.Provider)
	}
	maintenance, closeStore, err := provider.OpenMaintenance(ctx, maps.Clone(selection.Options))
	if closeStore != nil {
		defer func() { err = errors.Join(err, closeStore()) }()
	}
	if err != nil {
		return 0, fmt.Errorf("open account maintenance %q: %w", selection.Provider, err)
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if nilStorageService(maintenance) {
		return 0, fmt.Errorf("account provider %q returned no token maintenance", selection.Provider)
	}
	return maintenance.DeleteExpiredTokens(ctx, limit)
}

func selectedStorageProvider(name string, providers map[string]StorageProvider) (StorageProvider, error) {
	provider, ok := providers[name]
	if name == "" || !ok {
		return StorageProvider{}, fmt.Errorf("auth.accountStorage.provider: unknown provider %q", name)
	}
	return provider, nil
}

func nilStorageService(service any) bool {
	if service == nil {
		return true
	}
	value := reflect.ValueOf(service)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	}
	return false
}
