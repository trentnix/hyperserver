package user

import (
	"context"
	"errors"
	"testing"

	"github.com/trentnix/hyperserver/config"
)

type configuredAccounts struct{ AccountRepository }
type configuredTokens struct{ TokenMaintenance }

func TestConfiguredAccountOwnership(t *testing.T) {
	for _, mode := range []string{"success", "error", "nil", "typed nil", "canceled", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			selection := config.AccountStorageConfig{Provider: "custom", Options: map[string]string{"address": "original"}}
			if mode == "unknown" {
				selection.Provider = "missing"
			}
			cause, closeErr := errors.New("open failed"), errors.New("close failed")
			opens, closes := 0, 0
			repository := &configuredAccounts{}
			providers := map[string]StorageProvider{"custom": {Open: func(ctx context.Context, options map[string]string) (AccountRepository, func() error, error) {
				opens++
				options["address"] = "changed"
				close := func() error {
					closes++
					if mode == "error" {
						return closeErr
					}
					return nil
				}
				switch mode {
				case "error":
					return nil, close, cause
				case "nil":
					return nil, close, nil
				case "typed nil":
					return (*configuredAccounts)(nil), close, nil
				case "canceled":
					cancel()
				}
				return repository, close, nil
			}}}
			got, close, err := OpenConfiguredStorage(ctx, selection, providers)
			if mode == "success" {
				if err != nil || got != repository || close == nil || closes != 0 {
					t.Fatal("ownership not transferred", err)
				}
				if err := close(); err != nil {
					t.Fatal(err)
				}
			} else if err == nil || got != nil || close != nil {
				t.Fatal("failed open returned owned resources")
			}
			if mode == "error" && (!errors.Is(err, cause) || !errors.Is(err, closeErr)) {
				t.Fatal("lost failure", err)
			}
			if mode == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			want := 1
			if mode == "unknown" {
				want = 0
			}
			if opens != want || closes != want || selection.Options["address"] != "original" {
				t.Fatalf("opens=%d closes=%d options=%v", opens, closes, selection.Options)
			}
		})
	}
}

func TestConfiguredMaintenanceRejectsTypedNil(t *testing.T) {
	closed := 0
	providers := map[string]StorageProvider{"custom": {OpenMaintenance: func(context.Context, map[string]string) (TokenMaintenance, func() error, error) {
		return (*configuredTokens)(nil), func() error { closed++; return nil }, nil
	}}}
	if _, err := CleanupConfiguredTokens(t.Context(), config.AccountStorageConfig{Provider: "custom"}, providers, 1); err == nil || closed != 1 {
		t.Fatal("typed nil maintenance was not rejected and closed", err)
	}
}
