package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConstructorValidatesOptions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Options)
		want   string
	}{
		{"zero options", func(o *Options) { *o = Options{} }, "signing key"},
		{"empty key", func(o *Options) { o.SigningKey = "" }, "signing key"},
		{"short key", func(o *Options) { o.SigningKey = strings.Repeat("s", 31) }, "at least 32 bytes"},
		{"whitespace key", func(o *Options) { o.SigningKey = "  " }, "signing key"},
		{"placeholder key", func(o *Options) { o.SigningKey = "<replace-me>" }, "signing key"},
		{"zero token lifetime", func(o *Options) { o.TokenLifetime = 0 }, "token lifetime"},
		{"negative token lifetime", func(o *Options) { o.TokenLifetime = -time.Second }, "token lifetime"},
		{"subsecond token lifetime", func(o *Options) { o.TokenLifetime = time.Millisecond }, "token lifetime"},
		{"zero cookie lifetime", func(o *Options) { o.CookieLifetime = 0 }, "cookie lifetime"},
		{"negative cookie lifetime", func(o *Options) { o.CookieLifetime = -time.Second }, "cookie lifetime"},
		{"subsecond cookie lifetime", func(o *Options) { o.CookieLifetime = time.Millisecond }, "cookie lifetime"},
		{"empty connection", func(o *Options) { o.Connection = "" }, "not configured"},
		{"blank connection", func(o *Options) { o.Connection = "  " }, "not configured"},
		{"empty table", func(o *Options) { o.Table = "" }, "not configured"},
		{"blank table", func(o *Options) { o.Table = "  " }, "not configured"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sessions.db")
			options := Options{
				Connection:     path,
				Table:          "sessions",
				SigningKey:     strings.Repeat("s", 32),
				TokenLifetime:  time.Hour,
				CookieLifetime: time.Hour,
			}
			tc.change(&options)
			store, err := NewSQLiteStore(t.Context(), options)
			if store != nil {
				t.Cleanup(func() { store.Close() })
			}
			if store != nil || err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("constructor = %v, %v, want nil and %q", store, err, tc.want)
			}
			if options.SigningKey != "" && strings.Contains(err.Error(), options.SigningKey) {
				t.Fatal("error disclosed signing key")
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("invalid options created storage: %v", err)
			}
		})
	}
}

func TestConstructorCopiesOptions(t *testing.T) {
	options := Options{
		Connection:     ":memory:",
		Table:          "sessions",
		SigningKey:     strings.Repeat("s", 32),
		TokenLifetime:  time.Second,
		CookieLifetime: time.Second,
	}
	store, err := NewSQLiteStore(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	options.SigningKey = "changed"
	options.TokenLifetime = time.Hour
	options.CookieLifetime = time.Hour
	if string(store.JwtKey) != strings.Repeat("s", 32) || store.TokenLifetime != time.Second || store.CookieLifetime != time.Second {
		t.Fatal("constructor did not preserve validated options")
	}
}

func TestConstructorCanceledBeforeOpening(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	store, err := NewSQLiteStore(ctx, sqliteStoreOptions(path, "sessions"))
	if store != nil {
		t.Cleanup(func() { store.Close() })
	}
	if store != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("constructor = %v, %v, want nil and cancellation", store, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("canceled setup created storage: %v", err)
	}
}
