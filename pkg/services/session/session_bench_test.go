package session_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/session/cookie"
	"github.com/trentnix/hyperserver/pkg/services/session/sqlite"
)

// BenchmarkSession measures one saved 256-byte value. Each read starts a new
// request. repeated-get also reads it twice from that request's local cache.
func BenchmarkSession(b *testing.B) {
	for _, provider := range []string{"cookie", "sqlite"} {
		b.Run(provider, func(b *testing.B) {
			const key = "benchmark-only-key-not-a-secret00"
			factory := func(ctx context.Context) (session.SessionStore, error) {
				if provider == "cookie" {
					return cookie.NewCookieStore(cookie.Options{SigningKey: key, TokenLifetime: time.Hour, CookieLifetime: time.Hour})
				}
				return sqlite.NewSQLiteStore(ctx, sqlite.Options{
					Connection: filepath.Join(b.TempDir(), "sessions.db"), Table: "session",
					SigningKey: key, TokenLifetime: time.Hour, CookieLifetime: time.Hour,
				})
			}
			manager, err := session.NewSessionManager(b.Context(), map[string]string{"default": provider}, map[string]session.StoreFactory{provider: factory})
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() {
				if err := manager.Close(); err != nil {
					b.Error(err)
				}
			})
			request := session.AddSessionManagerToRequestContext(httptest.NewRequest(http.MethodGet, "https://example.invalid/", nil), manager)
			stored, err := session.New(request, "benchmark")
			if err != nil {
				b.Fatal(err)
			}
			stored.Data["value"] = strings.Repeat("x", 256)
			w := httptest.NewRecorder()
			if err := stored.Save(w, request); err != nil {
				b.Fatal(err)
			}
			base := httptest.NewRequest(http.MethodGet, "https://example.invalid/", nil)
			for _, c := range w.Result().Cookies() {
				base.AddCookie(c)
			}
			for _, operation := range []string{"get", "repeated-get", "save"} {
				b.Run(operation, func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						r := session.AddSessionManagerToRequestContext(base.Clone(base.Context()), manager)
						if operation == "save" {
							if err := stored.Save(httptest.NewRecorder(), r); err != nil {
								b.Fatal(err)
							}
							continue
						}
						got, err := session.Get(r, "benchmark")
						if err != nil || got == nil || got.ID != stored.ID || got.Data["value"] != stored.Data["value"] {
							b.Fatalf("load saved session: %v", err)
						}
						if operation == "repeated-get" {
							for range 2 {
								cached, err := session.Get(r, "benchmark")
								if err != nil || cached != got {
									b.Fatal("request-local session was not reused", err)
								}
							}
						}
					}
				})
			}
		})
	}
}
