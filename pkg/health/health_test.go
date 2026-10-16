package health

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/pkg/services/logger"
)

func TestReadinessLogsOnlyCheckName(t *testing.T) {
	var output bytes.Buffer
	l := slog.New(slog.NewJSONHandler(&output, nil)).With("requestID", "probe-123")
	h, err := Readiness(time.Second, Check{"storage", func(context.Context) error { return errors.New("private-token") }})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/health/ready", nil)
	r = r.WithContext(logger.Set(r.Context(), l))
	h.ServeHTTP(httptest.NewRecorder(), r)
	if strings.Contains(output.String(), "private-token") || !strings.Contains(output.String(), `"check":"storage"`) || !strings.Contains(output.String(), `"requestID":"probe-123"`) {
		t.Fatal(output.String())
	}
}

func TestReadiness(t *testing.T) {
	for _, tc := range []struct {
		name   string
		checks []Check
		status int
	}{
		{"stateless", nil, 204},
		{"healthy", []Check{{"storage", func(context.Context) error { return nil }}}, 204},
		{"failure", []Check{{"storage", func(context.Context) error { return errors.New("secret connection") }}}, 503},
		{"deadline", []Check{{"storage", func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }}}, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, err := Readiness(10*time.Millisecond, tc.checks...)
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", "/health/ready", nil))
			if w.Code != tc.status || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal(w)
			}
			if w.Body.String() != "" && w.Body.String() != "not ready\n" {
				t.Fatal("exposed provider details")
			}
		})
	}
}

func TestChecksAreValidatedAndCopied(t *testing.T) {
	for _, checks := range [][]Check{{{Name: "missing"}}, {{Run: func(context.Context) error { return nil }}}, {{"same", func(context.Context) error { return nil }}, {"same", func(context.Context) error { return nil }}}} {
		if _, err := Readiness(time.Second, checks...); err == nil {
			t.Fatal("accepted invalid checks")
		}
	}
	if _, err := Readiness(0); err == nil {
		t.Fatal("accepted zero timeout")
	}
	checks := []Check{{"ready", func(context.Context) error { return nil }}}
	h, err := Readiness(time.Second, checks...)
	if err != nil {
		t.Fatal(err)
	}
	checks[0].Run = nil
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 204 {
		t.Fatal(w.Code)
	}
}

func TestLivenessDoesNotNeedServices(t *testing.T) {
	w := httptest.NewRecorder()
	Liveness(w, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if w.Code != 204 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w)
	}
}

func TestCanceledReadinessWithoutChecks(t *testing.T) {
	h, err := Readiness(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil).WithContext(ctx))
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
}
