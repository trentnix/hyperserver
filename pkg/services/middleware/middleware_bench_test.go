package middleware

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/trentnix/hyperserver/pkg/requestinfo"
)

// BenchmarkMiddleware includes request cloning and response recording. Logging
// formats JSON into io.Discard, without measuring a disk or network log sink.
func BenchmarkMiddleware(b *testing.B) {
	proxy, err := requestinfo.NewProxyMiddleware(nil)
	if err != nil {
		b.Fatal(err)
	}
	hsts, err := NewHSTS(3600)
	if err != nil {
		b.Fatal(err)
	}
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	endpoint := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	chain := ChainMiddleware(endpoint,
		http.NewCrossOriginProtection().Handler,
		hsts,
		proxy,
		SecurityHeaders("default-src 'self'"),
		LoggerMiddleware(log),
	)
	for _, tc := range []struct {
		name    string
		handler http.Handler
	}{{"bare", endpoint}, {"security-and-logging", chain}} {
		b.Run(tc.name, func(b *testing.B) {
			r := httptest.NewRequest(http.MethodGet, "https://example.invalid/", nil)
			b.ReportAllocs()
			for b.Loop() {
				w := httptest.NewRecorder()
				tc.handler.ServeHTTP(w, r.Clone(r.Context()))
				if w.Code != http.StatusNoContent {
					b.Fatal(w.Code)
				}
			}
		})
	}
}
