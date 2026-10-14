package cookie

import (
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
		{"whitespace key", func(o *Options) { o.SigningKey = "  " }, "signing key"},
		{"placeholder key", func(o *Options) { o.SigningKey = "<replace-me>" }, "signing key"},
		{"zero token lifetime", func(o *Options) { o.TokenLifetime = 0 }, "token lifetime"},
		{"negative token lifetime", func(o *Options) { o.TokenLifetime = -time.Second }, "token lifetime"},
		{"subsecond token lifetime", func(o *Options) { o.TokenLifetime = time.Millisecond }, "token lifetime"},
		{"zero cookie lifetime", func(o *Options) { o.CookieLifetime = 0 }, "cookie lifetime"},
		{"negative cookie lifetime", func(o *Options) { o.CookieLifetime = -time.Second }, "cookie lifetime"},
		{"subsecond cookie lifetime", func(o *Options) { o.CookieLifetime = time.Millisecond }, "cookie lifetime"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options := Options{
				SigningKey:     strings.Repeat("s", 32),
				TokenLifetime:  time.Hour,
				CookieLifetime: time.Hour,
			}
			tc.change(&options)
			store, err := NewCookieStore(options)
			if store != nil || err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("constructor = %v, %v, want nil and %q", store, err, tc.want)
			}
		})
	}
}

func TestConstructorCopiesOptions(t *testing.T) {
	options := Options{
		SigningKey:     strings.Repeat("s", 32),
		TokenLifetime:  time.Second,
		CookieLifetime: time.Second,
	}
	store, err := NewCookieStore(options)
	if err != nil {
		t.Fatal(err)
	}
	options.SigningKey = "changed"
	options.TokenLifetime = time.Hour
	options.CookieLifetime = time.Hour
	if !store.IsEnabled() || string(store.JwtKey) != strings.Repeat("s", 32) || store.TokenLifetime != time.Second || store.CookieLifetime != time.Second {
		t.Fatal("constructor did not preserve validated options")
	}
}
